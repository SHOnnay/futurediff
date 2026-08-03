package ledgerrestore

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

func addTx(t *testing.T, r *ledger.Repository, id, root string) {
	t.Helper()
	_, err := r.Create(ledger.CreateInput{Transaction: domain.Transaction{ID: id, Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}, Workspace: domain.Workspace{TransactionID: id, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, id, "w"), ArtifactsPath: filepath.Join(root, id, "a"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestore(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	source, err := ledger.OpenRepository(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, source, "one", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = source.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	source.Close()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	addTx(t, live, "two", root)
	live.Close()
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	// Applying a backup that predates the live ledger must be refused.
	_, err = Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db")})
	if err == nil {
		t.Fatal("expected refusal for stale backup")
	}
	if !strings.Contains(err.Error(), "older than the live ledger") {
		t.Fatalf("unexpected error: %v", err)
	}
	report, err := Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db"), AllowStaleBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.PreRestoreBackup == nil {
		t.Fatalf("report=%+v", report)
	}
	restored, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	health, err := restored.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if health.TransactionCount != 1 {
		t.Fatalf("transactions=%d", health.TransactionCount)
	}
}

func TestRestore_CurrentBackupAllowed(t *testing.T) {
	// A backup that already contains every live chain is not stale and must be
	// applied without AllowStaleBackup.
	root := t.TempDir()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	addTx(t, live, "two", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = live.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	live.Close()
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db")})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("report=%+v", report)
	}
}

func TestRestore_LiveDaemonLockRefused(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	source, err := ledger.OpenRepository(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, source, "one", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = source.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	source.Close()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	live.Close()
	lockPath := filepath.Join(root, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, LockPath: lockPath, AllowStaleBackup: true})
	if err == nil {
		t.Fatal("expected refusal when a live daemon holds the lock")
	}
	if !strings.Contains(err.Error(), "daemon lock is held") {
		t.Fatalf("unexpected error: %v", err)
	}
}
