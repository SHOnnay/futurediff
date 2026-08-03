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
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Confirmation = "RESTORE_FUTUREDIFF_LEDGER"

type Options struct {
	LivePath             string
	BackupPath           string
	ExpectedSHA256       string
	SocketPath           string
	LockPath             string
	PreRestoreBackupPath string
	Apply                bool
	// AllowStaleBackup permits applying a backup that is older than the live
	// ledger (i.e. whose event chains are a strict prefix). Without it, a
	// restore that would discard committed effects is refused.
	AllowStaleBackup bool
	Confirmation     string
}

type Report struct {
	LivePath         string               `json:"live_path"`
	BackupPath       string               `json:"backup_path"`
	BackupSHA256     string               `json:"backup_sha256"`
	PreRestoreBackup *ledger.BackupRecord `json:"pre_restore_backup,omitempty"`
	Health           ledger.Health        `json:"health"`
	AuditHealthy     bool                 `json:"audit_healthy"`
	EventChainValid  bool                 `json:"event_chain_valid"`
	Applied          bool                 `json:"applied"`
	CompletedAt      time.Time            `json:"completed_at"`
}

func Run(opts Options) (Report, error) {
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
	if opts.Apply && opts.SocketPath != "" {
		if _, statErr := os.Lstat(opts.SocketPath); statErr == nil {
			return Report{}, errors.New("daemon socket exists; stop the daemon before restore")
		} else if !os.IsNotExist(statErr) {
			return Report{}, statErr
		}
	}
	if opts.Apply && opts.LockPath != "" {
		status, inspectErr := daemonlock.Inspect(opts.LockPath, time.Now())
		// Refuse while a live process holds the flock. ambiguous covers a live
		// owner whose daemon socket is unreachable; fail closed on both.
		if inspectErr == nil && status.LockStatus == "held" && (status.OwnerStatus == "alive" || status.OwnerStatus == "ambiguous") {
			return Report{}, errors.New("daemon lock is held by a live owner; stop the daemon before restore")
		}
	}
	dir := filepath.Dir(live)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Report{}, err
	}
	candidate, err := os.CreateTemp(dir, ".ledger-restore-*.db")
	if err != nil {
		return Report{}, err
	}
	candidatePath := candidate.Name()
	_ = candidate.Close()
	defer os.Remove(candidatePath)
	if err := copyFile(backup, candidatePath, 0o600); err != nil {
		return Report{}, err
	}
	candidateRepo, err := ledger.OpenRepository(candidatePath)
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
			// applying it would silently discard committed effects.
			liveRepo, openErr := ledger.OpenRepository(live)
			if openErr != nil {
				return Report{}, fmt.Errorf("open live ledger for staleness check: %w", openErr)
			}
			staleErr := backupCoversLive(candidatePath, liveRepo)
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
	if _, err := os.Stat(live); err == nil {
		repo, openErr := ledger.OpenRepository(live)
		if openErr != nil {
			return Report{}, fmt.Errorf("open live ledger: %w", openErr)
		}
		pre := opts.PreRestoreBackupPath
		if pre == "" {
			pre = filepath.Join(dir, fmt.Sprintf("ledger.pre-restore.%s.db", time.Now().UTC().Format("20060102T150405Z")))
		}
		rec, backupErr := repo.Backup(pre)
		closeLiveErr := repo.Close()
		if backupErr != nil {
			return Report{}, backupErr
		}
		if closeLiveErr != nil {
			return Report{}, closeLiveErr
		}
		report.PreRestoreBackup = &rec
	} else if !os.IsNotExist(err) {
		return Report{}, err
	}
	_ = os.Remove(live + "-wal")
	_ = os.Remove(live + "-shm")
	if err := os.Chmod(candidatePath, 0o600); err != nil {
		return Report{}, err
	}
	if err := os.Rename(candidatePath, live); err != nil {
		return Report{}, fmt.Errorf("publish restored ledger: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return Report{}, err
	}
	report.Applied = true
	report.CompletedAt = time.Now().UTC()
	return report, nil
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
