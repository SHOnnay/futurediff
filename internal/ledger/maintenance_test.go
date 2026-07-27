package ledger

import (
	"path/filepath"
	"testing"
)

func TestBackupAndIntegrity(t *testing.T) {
	root := t.TempDir()
	repo, err := OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	health, err := repo.HealthCheck()
	if err != nil || !health.IntegrityOK || health.MigrationCount < 8 {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	backupPath := filepath.Join(root, "backup", "ledger.db")
	record, err := repo.Backup(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if record.SHA256 == "" || record.SizeBytes == 0 {
		t.Fatalf("bad record: %+v", record)
	}
	backup, err := OpenRepository(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if _, err := backup.HealthCheck(); err != nil {
		t.Fatal(err)
	}
}
