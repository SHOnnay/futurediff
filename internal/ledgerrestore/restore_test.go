package ledgerrestore

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"testing"
	"time"
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
	report, err := Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db")})
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
