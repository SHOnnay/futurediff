package ledgerrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Confirmation = "RESTORE_FUTUREDIFF_LEDGER"

// Restore-boundary operation names consulted on a durablewrite.Injector in
// addition to the staging write boundaries owned by durablewrite (create,
// write, short_write, file_sync, rename, directory_sync). They follow the
// ADR-099 fault-injection convention: nothing outside tests constructs an
// injector.
const (
	OpQuarantineCreate     = "quarantine_create"
	OpPreserveLedger       = "preserve_ledger"
	OpPreserveWAL          = "preserve_wal"
	OpPreserveSHM          = "preserve_shm"
	OpPublishRename        = "publish_rename"
	OpPublishDirectorySync = "publish_directory_sync"
	OpPostVerify           = "post_verify"
)

type Options struct {
	LivePath       string
	BackupPath     string
	ExpectedSHA256 string
	SocketPath     string
	LockPath       string
	// PreRestoreBackupPath, when set, requests an additional semantic
	// pre-restore backup at this path. It is produced best-effort from a
	// private copy of the original so the authoritative ledger is never
	// opened; if the original cannot be copied or opened, the backup is
	// skipped and byte-for-byte preservation is the quarantine's job. When
	// empty, no standalone pre-restore backup file is written (the quarantine
	// is the preservation mechanism).
	PreRestoreBackupPath string
	Apply                bool
	// AllowStaleBackup permits applying a backup that is older than the live
	// ledger (i.e. whose event chains are a strict prefix). Without it, a
	// restore that would discard committed effects is refused. A damaged live
	// ledger cannot be compared at all and therefore always requires this
	// flag, matching the fail-closed behavior of the staleness check.
	AllowStaleBackup bool
	Confirmation     string
}

// PreservedOriginal describes how the authoritative ledger state that the
// restore replaced was preserved.
type PreservedOriginal struct {
	// LocationClass is "quarantine" when the original ledger (and any
	// present WAL/SHM sidecars) was copied byte-for-byte into a private
	// evidence directory, or "none" when no ledger existed to preserve.
	LocationClass string `json:"location_class"`
	// QuarantineDir is the private (0700) evidence directory holding the
	// pre-restore state. It is never deleted automatically and is never
	// overwritten by a later restore.
	QuarantineDir string `json:"quarantine_dir,omitempty"`
	// LedgerSHA256 is the digest of the preserved ledger.db copy.
	LedgerSHA256 string `json:"ledger_sha256,omitempty"`
	// WALPreserved and SHMPreserved report whether the pre-restore WAL and
	// SHM files were found at rest and preserved alongside the ledger.
	WALPreserved bool `json:"wal_preserved"`
	SHMPreserved bool `json:"shm_preserved"`
}

type Report struct {
	LivePath         string               `json:"live_path"`
	BackupPath       string               `json:"backup_path"`
	BackupSHA256     string               `json:"backup_sha256"`
	PreRestoreBackup *ledger.BackupRecord `json:"pre_restore_backup,omitempty"`
	Health           ledger.Health        `json:"health"`
	AuditHealthy     bool                 `json:"audit_healthy"`
	EventChainValid  bool                 `json:"event_chain_valid"`
	Preserved        *PreservedOriginal   `json:"preserved_original,omitempty"`
	RestoredHealthy  bool                 `json:"restored_healthy"`
	// RestoreVerification is the reason code of the post-restore offline
	// diagnosis ("healthy" on success).
	RestoreVerification string    `json:"restore_verification,omitempty"`
	Applied             bool      `json:"applied"`
	CompletedAt         time.Time `json:"completed_at"`
}

// Run validates and optionally applies a ledger restore. Apply restores only
// when the daemon is conclusively stopped, the backup is verified and
// belongs to the same data root, the operator confirmed the restore, and the
// destination is writable. See RunWithInjector for the staged-replacement
// ordering.
func Run(opts Options) (Report, error) {
	return RunWithInjector(opts, nil)
}

// RunWithInjector is Run with a test-only durable-write fault injector
// (ADR-099). Production callers use Run; nothing outside tests constructs an
// injector. The injector is consulted at the staging create/write/short_write/
// file_sync/rename/directory_sync boundaries and at the restore-specific
// quarantine, preserve, publish, and post-verification boundaries.
//
// The apply flow is a same-filesystem staged replacement:
//
//  1. verify backup digest, confirmation, daemon stop, provenance, and the
//     staged copy;
//  2. create a private staging file in the live ledger's own directory
//     (same filesystem by construction, so the publish rename is atomic);
//  3. copy the verified backup into staging and apply safe permissions;
//  4. sync staging (via durablewrite) and fold any validation WAL frames
//     back into it;
//  5. preserve the current ledger.db and any WAL/SHM sidecars byte-for-byte
//     in a uniquely named private quarantine directory;
//  6. atomically rename staging over the authoritative ledger path;
//  7. sync the parent directory;
//  8. diagnose the restored ledger offline and refuse to report success
//     unless it is healthy.
//
// Until step 6 the old authoritative state is never touched. Failures before
// quarantine leave no trace; failures during preservation remove the partial
// quarantine and leave the old state authoritative; failures at or after
// publish retain the completed quarantine evidence and are reported without
// a false success. The quarantine is never deleted automatically.
func RunWithInjector(opts Options, inject durablewrite.Injector) (Report, error) {
	live, err := cleanPath(opts.LivePath)
	if err != nil {
		return Report{}, fmt.Errorf("live path: %w", err)
	}
	backup, err := cleanExistingRegular(opts.BackupPath)
	if err != nil {
		return Report{}, fmt.Errorf("backup path: %w", err)
	}
	if same, _ := filepath.EvalSymlinks(live); same != "" && same == backup {
		return Report{}, errors.New("live and backup paths must differ")
	}
	// Never follow a symlinked or otherwise non-regular live ledger: the
	// replacement must target the authoritative file itself.
	if st, statErr := os.Lstat(live); statErr == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			return Report{}, fmt.Errorf("live ledger %s must be a regular non-symlink file", live)
		}
	} else if !os.IsNotExist(statErr) {
		return Report{}, statErr
	}
	digest, err := shaFile(backup)
	if err != nil {
		return Report{}, err
	}
	if opts.ExpectedSHA256 != "" && !strings.EqualFold(opts.ExpectedSHA256, digest) {
		return Report{}, errors.New("backup SHA-256 does not match expected digest")
	}
	if opts.Apply && opts.ExpectedSHA256 == "" {
		return Report{}, errors.New("--expected-sha256 is required when applying a restore")
	}
	if opts.Apply && opts.Confirmation != Confirmation {
		return Report{}, errors.New("restore confirmation phrase is missing or incorrect")
	}
	dir := filepath.Dir(live)
	// Provenance: the backup must belong to the same FutureDiff data root as
	// the live ledger. A backup that resolves outside the root is refused.
	if !withinRoot(dir, backup) {
		return Report{}, fmt.Errorf("backup %s is outside the FutureDiff data root %s", backup, dir)
	}
	if opts.Apply && opts.SocketPath != "" {
		if _, statErr := os.Lstat(opts.SocketPath); statErr == nil {
			return Report{}, errors.New("daemon socket exists; stop the daemon before restore")
		} else if !os.IsNotExist(statErr) {
			return Report{}, statErr
		}
	}
	if opts.Apply && opts.LockPath != "" {
		if _, statErr := os.Lstat(opts.LockPath); statErr == nil {
			status, inspectErr := daemonlock.Inspect(opts.LockPath, time.Now())
			// Fail closed: an un-inspectable lock means no live or ambiguous
			// owner can be ruled out.
			if inspectErr != nil {
				return Report{}, fmt.Errorf("daemon lock could not be inspected: %w", inspectErr)
			}
			if status.LockStatus == "held" && (status.OwnerStatus == "alive" || status.OwnerStatus == "ambiguous") {
				return Report{}, errors.New("daemon lock is held by a live owner; stop the daemon before restore")
			}
		} else if !os.IsNotExist(statErr) {
			return Report{}, statErr
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Report{}, err
	}
	if opts.Apply {
		if err := cleanAbandonedStaging(dir, live, backup); err != nil {
			return Report{}, err
		}
	}
	// Stage the verified backup into a private file in the live directory:
	// same filesystem as the authoritative path, so the publish rename is an
	// atomic same-device replacement.
	staging := filepath.Join(dir, ".ledger-restore-"+domain.NewID("staged")+".db")
	defer func() {
		// Staging is a private disposable file; remove it and any SQLite
		// sidecars the validation opens created next to it on every path
		// (dry-run, refusal, fault, and after publish).
		os.Remove(staging)
		os.Remove(staging + "-wal")
		os.Remove(staging + "-shm")
	}()
	if err := durablewrite.ReplaceFileVia(staging, 0o600, inject, func(tmp string) error {
		return copyFile(backup, tmp, 0o600)
	}); err != nil {
		return Report{}, fmt.Errorf("stage backup: %w", err)
	}
	candidateRepo, err := ledger.OpenRepository(staging)
	if err != nil {
		return Report{}, fmt.Errorf("backup validation: %w", err)
	}
	health, healthErr := candidateRepo.HealthCheck()
	audit, auditErr := candidateRepo.Audit()
	chain, chainErr := candidateRepo.VerifyEventChains()
	closeErr := candidateRepo.Close()
	if healthErr != nil {
		return Report{}, healthErr
	}
	if auditErr != nil {
		return Report{}, auditErr
	}
	if chainErr != nil {
		return Report{}, chainErr
	}
	if closeErr != nil {
		return Report{}, closeErr
	}
	if !audit.Healthy || !chain.Valid {
		return Report{}, errors.New("backup failed semantic validation")
	}
	if opts.Apply && !opts.AllowStaleBackup {
		if _, statErr := os.Stat(live); statErr == nil {
			// Refuse to replace a newer live ledger with an older backup:
			// applying it would silently discard committed effects. A damaged
			// live ledger cannot be compared and requires AllowStaleBackup.
			liveRepo, openErr := ledger.OpenRepository(live)
			if openErr != nil {
				return Report{}, fmt.Errorf("open live ledger for staleness check: %w", openErr)
			}
			staleErr := backupCoversLive(staging, liveRepo)
			liveCloseErr := liveRepo.Close()
			if staleErr != nil {
				return Report{}, staleErr
			}
			if liveCloseErr != nil {
				return Report{}, liveCloseErr
			}
		} else if !os.IsNotExist(statErr) {
			return Report{}, statErr
		}
	}
	report := Report{LivePath: live, BackupPath: backup, BackupSHA256: digest, Health: health, AuditHealthy: audit.Healthy, EventChainValid: chain.Valid, CompletedAt: time.Now().UTC()}
	if !opts.Apply {
		return report, nil
	}
	// Fold any WAL frames produced by the validation and staleness opens back
	// into the staged file, so the published ledger is a complete single file
	// and no orphaned sidecar is left behind.
	if err := foldSidecars(staging); err != nil {
		return Report{}, err
	}
	// Preserve the current authoritative state (ledger.db and any WAL/SHM
	// sidecars) byte-for-byte before any replacement happens.
	preserved, err := quarantineOriginal(live, inject)
	if err != nil {
		return Report{}, err
	}
	report.Preserved = preserved
	// Optional semantic pre-restore backup (existing behavior): produced
	// best-effort from a private copy so the authoritative ledger is never
	// opened; the quarantine remains the byte-for-byte preservation mechanism.
	if err := preserveSemanticBackup(opts, live, dir, &report); err != nil {
		return Report{}, err
	}
	if inject != nil {
		if err := inject.Before(OpPublishRename); err != nil {
			return Report{}, fmt.Errorf("publish restored ledger: %w", err)
		}
	}
	if err := os.Rename(staging, live); err != nil {
		return Report{}, fmt.Errorf("publish restored ledger: %w", err)
	}
	if inject != nil {
		if err := inject.Before(OpPublishDirectorySync); err != nil {
			return Report{}, fmt.Errorf("publish restored ledger: %w", err)
		}
	}
	if err := syncDir(dir); err != nil {
		return Report{}, fmt.Errorf("publish restored ledger: %w", err)
	}
	// The restored ledger is verified offline before success is reported.
	if inject != nil {
		if err := inject.Before(OpPostVerify); err != nil {
			return Report{}, fmt.Errorf("post-restore verification: %w", err)
		}
	}
	diag, err := ledger.Diagnose(live, ledger.DiagnoseOptions{Quiescent: true})
	if err != nil {
		return Report{}, fmt.Errorf("post-restore verification: %w", err)
	}
	if diag.State != ledger.Healthy {
		return Report{}, fmt.Errorf("post-restore verification failed: %s (%s)", diag.ReasonCode, diag.Message)
	}
	report.RestoredHealthy = true
	report.RestoreVerification = diag.ReasonCode
	if report.RestoreVerification == "" {
		report.RestoreVerification = string(diag.State)
	}
	report.Applied = true
	report.CompletedAt = time.Now().UTC()
	return report, nil
}

// Classify maps a restore failure to a stable storage reason code using the
// existing classification vocabulary: the durablewrite sentinels (and the
// underlying errnos) map to disk_full, quota_exceeded, and
// filesystem_read_only, and EIO-class failures map to the ledger
// storage_io_failure code. An empty string means the error is not a
// classified storage failure. Wrapped errors are unwrapped via errors.Is, so
// real errno wrappers classify identically.
func Classify(err error) string {
	switch {
	case errors.Is(err, durablewrite.ErrDiskFull) || errors.Is(err, syscall.ENOSPC):
		return "disk_full"
	case errors.Is(err, durablewrite.ErrQuotaExceeded) || errors.Is(err, syscall.EDQUOT):
		return "quota_exceeded"
	case errors.Is(err, durablewrite.ErrReadOnlyFilesystem) || errors.Is(err, syscall.EROFS):
		return "filesystem_read_only"
	case errors.Is(err, durablewrite.ErrIO) || errors.Is(err, syscall.EIO):
		return "storage_io_failure"
	}
	return ""
}

func cleanPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	return filepath.Abs(path)
}
func cleanExistingRegular(path string) (string, error) {
	abs, e := cleanPath(path)
	if e != nil {
		return "", e
	}
	info, e := os.Lstat(abs)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("must be a regular file")
	}
	return abs, nil
}

// withinRoot reports whether path resolves inside root, after canonicalizing
// both sides (symlinks resolved where possible) so a symlink escaping the
// root is not treated as belonging to it.
func withinRoot(root, path string) bool {
	canonical := func(p string) string {
		abs, err := filepath.Abs(filepath.Clean(p))
		if err != nil {
			return filepath.Clean(p)
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved
		}
		return abs
	}
	r := canonical(root)
	p := canonical(path)
	rel, e := filepath.Rel(r, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}

// cleanAbandonedStaging removes staging files left behind by a previously
// crashed restore in the live directory. Only files matching the restore
// staging pattern are touched; the live ledger and the selected backup are
// never removed, and unrelated files are never touched. Restore is a
// serialized operator action (confirmation phrase, daemon stopped), so
// leftovers are safe to clean.
func cleanAbandonedStaging(dir, live, backup string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".ledger-restore-") {
			continue
		}
		if !(strings.HasSuffix(name, ".db") || strings.Contains(name, ".tmp-")) {
			continue
		}
		p := filepath.Join(dir, name)
		if p == live || p == backup {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clean abandoned staging file %s: %w", p, err)
		}
	}
	return nil
}

// quarantineOriginal copies the authoritative ledger.db and any WAL/SHM
// sidecars byte-for-byte into a uniquely named private (0700) evidence
// directory inside the data root. Sidecar files must be regular non-symlink
// files; anything else fails the restore closed rather than being followed.
// A partial quarantine is removed; a completed quarantine is never deleted
// automatically and is never overwritten by later restores (unique names).
func quarantineOriginal(live string, inject durablewrite.Injector) (*PreservedOriginal, error) {
	st, err := os.Lstat(live)
	if err != nil {
		if os.IsNotExist(err) {
			return &PreservedOriginal{LocationClass: "none"}, nil
		}
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("live ledger %s must be a regular non-symlink file", live)
	}
	if inject != nil {
		if err := inject.Before(OpQuarantineCreate); err != nil {
			return nil, err
		}
	}
	dir := filepath.Dir(live)
	q := filepath.Join(dir, "ledger-restore-evidence-"+domain.NewID("q"))
	if err := os.Mkdir(q, 0o700); err != nil {
		return nil, err
	}
	preserved := &PreservedOriginal{LocationClass: "quarantine", QuarantineDir: q}
	fail := func(err error) (*PreservedOriginal, error) {
		_ = os.RemoveAll(q)
		return nil, err
	}
	if inject != nil {
		if err := inject.Before(OpPreserveLedger); err != nil {
			return fail(err)
		}
	}
	if err := copyFile(live, filepath.Join(q, "ledger.db"), 0o600); err != nil {
		return fail(fmt.Errorf("preserve live ledger: %w", err))
	}
	sum, err := shaFile(filepath.Join(q, "ledger.db"))
	if err != nil {
		return fail(err)
	}
	preserved.LedgerSHA256 = sum
	sidecars := []struct {
		suffix string
		name   string
		op     string
		flag   *bool
	}{
		{"-wal", "ledger.db-wal", OpPreserveWAL, &preserved.WALPreserved},
		{"-shm", "ledger.db-shm", OpPreserveSHM, &preserved.SHMPreserved},
	}
	for _, sc := range sidecars {
		sidecar := live + sc.suffix
		sst, serr := os.Lstat(sidecar)
		if serr != nil {
			if os.IsNotExist(serr) {
				continue
			}
			return fail(serr)
		}
		if sst.Mode()&os.ModeSymlink != 0 || !sst.Mode().IsRegular() {
			return fail(fmt.Errorf("live sidecar %s must be a regular non-symlink file", sidecar))
		}
		if inject != nil {
			if err := inject.Before(sc.op); err != nil {
				return fail(err)
			}
		}
		if err := copyFile(sidecar, filepath.Join(q, sc.name), 0o600); err != nil {
			return fail(fmt.Errorf("preserve live sidecar %s: %w", sidecar, err))
		}
		*sc.flag = true
	}
	if err := syncDir(q); err != nil {
		return fail(fmt.Errorf("sync quarantine directory: %w", err))
	}
	return preserved, nil
}

// preserveSemanticBackup produces the optional pre-restore backup (existing
// CLI behavior) from a private throwaway copy of the original at-rest state
// (ledger.db plus any WAL/SHM sidecars). The authoritative ledger and the
// completed quarantine evidence are never opened or modified, so a damaged
// original is preserved byte-for-byte no matter how the semantic backup turns
// out. The backup is best-effort: if the copy cannot be opened or backed up,
// the restore proceeds with the quarantine as the preservation mechanism and
// report.PreRestoreBackup stays nil.
func preserveSemanticBackup(opts Options, live, dir string, report *Report) error {
	if _, err := os.Stat(live); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	scratch, err := os.MkdirTemp(dir, ".ledger-restore-semantic-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(scratch)
	copyInto := func(src, dstName string) error {
		info, err := os.Lstat(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(src, filepath.Join(scratch, dstName), 0o600)
	}
	if err := copyInto(live, "ledger.db"); err != nil {
		return nil
	}
	if err := copyInto(live+"-wal", "ledger.db-wal"); err != nil {
		return nil
	}
	if err := copyInto(live+"-shm", "ledger.db-shm"); err != nil {
		return nil
	}
	repo, err := ledger.OpenRepository(filepath.Join(scratch, "ledger.db"))
	if err != nil {
		return nil
	}
	pre := opts.PreRestoreBackupPath
	if pre == "" {
		// Unique suffix: repeated restores must never overwrite a previous
		// pre-restore backup.
		pre = filepath.Join(dir, fmt.Sprintf("ledger.pre-restore.%s.%s.db", time.Now().UTC().Format("20060102T150405Z"), domain.NewID("p")))
	}
	rec, backupErr := repo.Backup(pre)
	closeErr := repo.Close()
	if backupErr != nil {
		return nil
	}
	if closeErr != nil {
		return nil
	}
	report.PreRestoreBackup = &rec
	return nil
}

// foldSidecars checkpoints any WAL frames produced by the validation opens
// back into the staged file and removes the staged sidecars, so the file that
// is published is a complete, self-contained SQLite database.
func foldSidecars(path string) error {
	db, err := ledger.Open(path)
	if err != nil {
		return fmt.Errorf("fold staged sidecars: %w", err)
	}
	checkErr := db.Checkpoint()
	closeErr := db.Close()
	if checkErr != nil {
		return fmt.Errorf("fold staged sidecars: %w", checkErr)
	}
	if closeErr != nil {
		return fmt.Errorf("fold staged sidecars: %w", closeErr)
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	return syncFile(path)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if e != nil {
		return e
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(dst)
		}
	}()
	if _, e = io.Copy(out, in); e != nil {
		return e
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	ok = true
	return nil
}
func shaFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func syncFile(path string) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func syncDir(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// backupCoversLive reports whether the backup at backupPath contains every
// event chain that the live repository has, at an equal or greater sequence.
// A backup that is missing chains or has shorter chains is older than the live
// ledger: applying it would discard committed effects.
func backupCoversLive(backupPath string, live *ledger.Repository) error {
	backupRepo, err := ledger.OpenRepository(backupPath)
	if err != nil {
		return fmt.Errorf("reopen backup for staleness check: %w", err)
	}
	defer backupRepo.Close()
	backupHeads, err := backupRepo.EventChainHeads()
	if err != nil {
		return fmt.Errorf("backup event heads: %w", err)
	}
	liveHeads, err := live.EventChainHeads()
	if err != nil {
		return fmt.Errorf("live event heads: %w", err)
	}
	byTransaction := make(map[string]int64, len(backupHeads.Heads))
	for _, h := range backupHeads.Heads {
		byTransaction[h.TransactionID] = h.Sequence
	}
	var older []string
	for _, h := range liveHeads.Heads {
		backupSeq, ok := byTransaction[h.TransactionID]
		if !ok {
			older = append(older, fmt.Sprintf("%s (missing in backup)", h.TransactionID))
			continue
		}
		if backupSeq < h.Sequence {
			older = append(older, fmt.Sprintf("%s (live=%d backup=%d)", h.TransactionID, h.Sequence, backupSeq))
		}
	}
	if len(older) > 0 {
		return fmt.Errorf("backup is older than the live ledger for %d chain(s): %s; restoring would discard committed effects (pass AllowStaleBackup to override)", len(older), strings.Join(older, ", "))
	}
	return nil
}
