package ledgerrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	OpQuarantineCreate       = "quarantine_create"
	OpPreserveLedger         = "preserve_ledger"
	OpPreserveWAL            = "preserve_wal"
	OpPreserveSHM            = "preserve_shm"
	OpPreserveLedgerSync     = "preserve_ledger_sync"
	OpPreserveWALSync        = "preserve_wal_sync"
	OpPreserveSHMSync        = "preserve_shm_sync"
	OpQuarantineEvidenceSync = "quarantine_evidence_sync"
	OpQuarantineDirSync      = "quarantine_dir_sync"
	OpQuarantineParentSync   = "quarantine_parent_sync"
	OpRemoveLiveSidecars     = "remove_live_sidecars"
	OpPublishRename          = "publish_rename"
	OpPublishDirectorySync   = "publish_directory_sync"
	OpPostVerify             = "post_verify"
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
	RestoreVerification string `json:"restore_verification,omitempty"`
	Applied             bool   `json:"applied"`
	// AlreadyRestored reports that the authoritative ledger was already
	// byte-identical to the verified backup, so no replacement was performed
	// and no new evidence was created; Applied stays false in that case.
	AlreadyRestored bool      `json:"already_restored,omitempty"`
	CompletedAt     time.Time `json:"completed_at"`
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
// Provenance binds the backup to this FutureDiff home two ways: the backup
// must resolve inside the live ledger's data root, and every backup recorded
// inside the backup file's own ledger_backups catalog must also resolve
// inside that root (with a digest that still matches the on-disk file when
// the referenced backup still exists). A backup that was produced by another
// home carries that home's catalog entries and is refused. A first backup of
// a home records nothing and passes vacuously; that residual cannot be bound
// further when the damaged live ledger is unreadable, so it remains gated by
// the operator's explicit confirmation and digest.
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
//  5. if the authoritative ledger is already byte-identical to the verified
//     backup (a prior restore succeeded), report AlreadyRestored after
//     re-syncing the parent directory and re-verifying the ledger offline,
//     without creating new evidence;
//  6. preserve the current ledger.db and any WAL/SHM sidecars byte-for-byte
//     in a uniquely named private quarantine directory, fsyncing every
//     evidence file, the quarantine directory, and the parent directory
//     before any replacement; every file- and directory-sync failure aborts
//     before publication and removes only the partial quarantine;
//  7. remove the pre-restore WAL/SHM from the authoritative path (their
//     bytes are preserved in quarantine) so the restored ledger never
//     coexists with stale sidecars;
//  8. atomically rename staging over the authoritative ledger path and sync
//     the parent directory;
//  9. diagnose the restored ledger offline and refuse to report success
//     unless it is healthy.
//
// Until step 8 the old authoritative state is never touched. Failures before
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
	backupRecords, recordsErr := candidateRepo.Backups()
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
	if recordsErr != nil {
		return Report{}, fmt.Errorf("read backup catalog: %w", recordsErr)
	}
	if closeErr != nil {
		return Report{}, closeErr
	}
	if !audit.Healthy || !chain.Valid {
		return Report{}, errors.New("backup failed semantic validation")
	}
	// Provenance binding: every backup recorded inside the backup file's own
	// catalog must belong to this data root and still match the on-disk file.
	// This refuses a valid ledger copied in from another FutureDiff home.
	if err := verifyBackupLineage(dir, backupRecords); err != nil {
		return Report{}, err
	}
	report := Report{LivePath: live, BackupPath: backup, BackupSHA256: digest, Health: health, AuditHealthy: audit.Healthy, EventChainValid: chain.Valid, CompletedAt: time.Now().UTC()}
	if !opts.Apply {
		return report, nil
	}
	// Already-restored detection: when the authoritative ledger is
	// byte-identical to the verified backup, a prior restore already applied.
	// Re-sync the parent directory and re-verify the ledger offline so an
	// ambiguous earlier publish (e.g. a failed parent-dir sync or post-verify
	// step) is repaired, then return a stable result without touching the
	// ledger and without creating new evidence. Only byte-identity counts as
	// already-restored: chain equality alone would miss ledger_backups or
	// settings rows that a replacement would discard.
	if liveSHA, err := shaFile(live); err == nil && strings.EqualFold(liveSHA, digest) {
		if inject != nil {
			if err := inject.Before(OpPublishDirectorySync); err != nil {
				return Report{}, fmt.Errorf("already-restored durability sync: %w", err)
			}
		}
		if err := syncDir(dir); err != nil {
			return Report{}, fmt.Errorf("already-restored durability sync: %w", err)
		}
		if inject != nil {
			if err := inject.Before(OpPostVerify); err != nil {
				return Report{}, fmt.Errorf("already-restored verification: %w", err)
			}
		}
		diag, err := ledger.Diagnose(live, ledger.DiagnoseOptions{Quiescent: true})
		if err != nil {
			return Report{}, fmt.Errorf("already-restored verification: %w", err)
		}
		if diag.State != ledger.Healthy {
			return Report{}, fmt.Errorf("already-restored verification failed: %s (%s)", diag.ReasonCode, diag.Message)
		}
		report.AlreadyRestored = true
		report.RestoredHealthy = true
		report.RestoreVerification = diag.ReasonCode
		if report.RestoreVerification == "" {
			report.RestoreVerification = string(diag.State)
		}
		report.CompletedAt = time.Now().UTC()
		return report, nil
	}
	if !opts.AllowStaleBackup {
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
	// Fold any WAL frames produced by the validation and staleness opens back
	// into the staged file, so the published ledger is a complete single file
	// and no orphaned sidecar is left behind.
	if err := foldSidecars(staging); err != nil {
		return Report{}, err
	}
	// Preserve the current authoritative state (ledger.db and any WAL/SHM
	// sidecars) byte-for-byte before any replacement happens. Every evidence
	// file, the quarantine directory, and the parent directory are fsynced
	// before publication; a durability failure aborts and removes only the
	// partial quarantine.
	preserved, err := quarantineOriginal(live, digest, inject)
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
	// The pre-restore WAL/SHM sidecars belong to the original ledger, whose
	// bytes are preserved in quarantine. Remove them from the authoritative
	// path before publishing so the restored ledger never coexists with stale
	// sidecars.
	if err := removeLiveSidecars(live, inject); err != nil {
		return Report{}, fmt.Errorf("publish restored ledger: %w", err)
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

// cleanAbandonedStaging removes staging files and scratch directories left
// behind by a previously crashed restore in the live directory. Only files
// matching the restore staging patterns are touched; the live ledger and the
// selected backup are never removed, and unrelated files are never touched.
// Restore is a serialized operator action (confirmation phrase, daemon
// stopped), so leftovers are safe to clean.
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
		p := filepath.Join(dir, name)
		if p == live || p == backup {
			continue
		}
		// Abandoned semantic pre-restore scratch directories (a crashed
		// semantic backup leaves the whole private directory behind).
		if strings.HasPrefix(name, ".ledger-restore-semantic-") && e.IsDir() {
			if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("clean abandoned staging directory %s: %w", p, err)
			}
			continue
		}
		if !(strings.HasSuffix(name, ".db") || strings.Contains(name, ".tmp-")) {
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
//
// Durability ordering: every copied evidence file is fsynced, an evidence
// manifest (evidence.json) is written and fsynced, the quarantine directory
// is fsynced, and the live parent directory is fsynced so the quarantine
// entry itself is durable — all before any replacement of the authoritative
// ledger. Any file- or directory-sync failure aborts the restore before
// publication and removes only the partial quarantine; the original ledger
// remains authoritative. A completed quarantine is never deleted
// automatically and is never overwritten by later restores (unique names).
func quarantineOriginal(live, backupDigest string, inject durablewrite.Injector) (*PreservedOriginal, error) {
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
	// The evidence files are written without an internal fsync so each
	// file-sync boundary is independently fault-injectable; the sync happens
	// explicitly right after, before the quarantine can be considered durable.
	copyEvidence := func(src, dst, op string) error {
		if err := copyFileContents(src, dst, 0o600); err != nil {
			return err
		}
		if inject != nil {
			if err := inject.Before(op); err != nil {
				return err
			}
		}
		return syncFile(dst)
	}
	if inject != nil {
		if err := inject.Before(OpPreserveLedger); err != nil {
			return fail(err)
		}
	}
	if err := copyEvidence(live, filepath.Join(q, "ledger.db"), OpPreserveLedgerSync); err != nil {
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
		syncOp string
		flag   *bool
	}{
		{"-wal", "ledger.db-wal", OpPreserveWAL, OpPreserveWALSync, &preserved.WALPreserved},
		{"-shm", "ledger.db-shm", OpPreserveSHM, OpPreserveSHMSync, &preserved.SHMPreserved},
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
		if err := copyEvidence(sidecar, filepath.Join(q, sc.name), sc.syncOp); err != nil {
			return fail(fmt.Errorf("preserve live sidecar %s: %w", sidecar, err))
		}
		*sc.flag = true
	}
	// The evidence manifest makes the quarantine self-describing and is
	// itself written and fsynced before the directory syncs.
	manifest := evidenceManifest{
		Version:      1,
		PreservedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		LedgerSHA256: preserved.LedgerSHA256,
		BackupSHA256: backupDigest,
		WALPreserved: preserved.WALPreserved,
		SHMPreserved: preserved.SHMPreserved,
	}
	if err := writeEvidenceManifest(q, manifest, inject); err != nil {
		return fail(err)
	}
	if inject != nil {
		if err := inject.Before(OpQuarantineDirSync); err != nil {
			return fail(err)
		}
	}
	if err := syncDir(q); err != nil {
		return fail(fmt.Errorf("sync quarantine directory: %w", err))
	}
	// The parent directory must be synced too, or a crash could lose the
	// quarantine directory entry itself despite the synced contents.
	if inject != nil {
		if err := inject.Before(OpQuarantineParentSync); err != nil {
			return fail(err)
		}
	}
	if err := syncDir(dir); err != nil {
		return fail(fmt.Errorf("sync live directory for quarantine: %w", err))
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
// report.PreRestoreBackup stays nil. No standalone file is written unless
// Options.PreRestoreBackupPath is set — the quarantine is the default
// preservation mechanism, and repeated restores must not accumulate
// auto-named pre-restore files in the data root.
func preserveSemanticBackup(opts Options, live, dir string, report *Report) error {
	if opts.PreRestoreBackupPath == "" {
		return nil
	}
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

// copyFileContents copies src to dst (mode applied) without an internal
// fsync, so callers can fsync at an independently fault-injectable boundary.
func copyFileContents(src, dst string, mode os.FileMode) error {
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
	if e = out.Close(); e != nil {
		return e
	}
	ok = true
	return nil
}

// copyFile copies src to dst (mode applied) and fsyncs the copy. It is used
// for the staged backup copy and the semantic pre-restore scratch copy.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := copyFileContents(src, dst, mode); err != nil {
		return err
	}
	return syncFile(dst)
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

// verifyBackupLineage binds the verified backup to this FutureDiff home using
// the repository-controlled backup metadata recorded inside the backup file
// itself (its ledger_backups table). Every referenced backup must resolve
// inside the data root; a referenced file that still exists must still match
// the recorded size and digest. This refuses a valid ledger copied in from
// another home (its internal catalog points at that home's root) and a
// doctored catalog. A backup that records nothing (a home's first backup)
// passes vacuously: that residual cannot be bound further when the damaged
// live ledger is unreadable, so it remains gated by the operator's explicit
// confirmation and digest.
func verifyBackupLineage(dir string, records []ledger.BackupRecord) error {
	for _, rec := range records {
		if !withinRoot(dir, rec.Path) {
			return fmt.Errorf("backup ledger references backup %s outside the FutureDiff data root %s", rec.Path, dir)
		}
		st, err := os.Lstat(rec.Path)
		if err != nil {
			if os.IsNotExist(err) {
				// The referenced backup was already deleted (e.g. by
				// retention); lineage is still consistent.
				continue
			}
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
			continue
		}
		if st.Size() != rec.SizeBytes {
			return fmt.Errorf("backup ledger references backup %s with size %d, on-disk size %d", rec.Path, rec.SizeBytes, st.Size())
		}
		sum, err := shaFile(rec.Path)
		if err != nil {
			return err
		}
		if !strings.EqualFold(sum, rec.SHA256) {
			return fmt.Errorf("backup ledger references backup %s with digest %s that does not match the on-disk file", rec.Path, rec.SHA256)
		}
	}
	return nil
}

// removeLiveSidecars removes the pre-restore WAL/SHM sidecars from the
// authoritative path. Their bytes were preserved byte-for-byte in the
// quarantine before this point, so removal is safe; the restored ledger must
// never coexist with stale sidecars belonging to the replaced ledger.
func removeLiveSidecars(live string, inject durablewrite.Injector) error {
	if inject != nil {
		if err := inject.Before(OpRemoveLiveSidecars); err != nil {
			return err
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		p := live + suffix
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// evidenceManifest is the self-describing metadata written into each
// quarantine directory before the directory syncs. It records what was
// preserved, when, and against which verified backup.
type evidenceManifest struct {
	Version      int    `json:"version"`
	PreservedAt  string `json:"preserved_at"`
	LedgerSHA256 string `json:"ledger_sha256"`
	BackupSHA256 string `json:"backup_sha256"`
	WALPreserved bool   `json:"wal_preserved"`
	SHMPreserved bool   `json:"shm_preserved"`
}

// writeEvidenceManifest writes evidence.json into the quarantine directory
// and fsyncs it (the file-sync boundary is independently fault-injectable).
func writeEvidenceManifest(q string, m evidenceManifest, inject durablewrite.Injector) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(q, "evidence.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if inject != nil {
		if err := inject.Before(OpQuarantineEvidenceSync); err != nil {
			return err
		}
	}
	return syncFile(path)
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
