package backupcatalog

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogDetectsTamper(t *testing.T) {
	root := t.TempDir()
	repo, e := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer repo.Close()
	backupRoot := filepath.Join(root, "backups")
	if e = os.MkdirAll(backupRoot, 0700); e != nil {
		t.Fatal(e)
	}
	b, e := repo.Backup(filepath.Join(backupRoot, "one.db"))
	if e != nil {
		t.Fatal(e)
	}
	p := Policy{Version: Version, BackupRoot: backupRoot, KeepLatest: 0, MinimumAgeHours: 0}
	report, e := Evaluate(repo, p, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if !report.Healthy || len(report.Candidates) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if e = os.WriteFile(b.Path, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	report, e = Evaluate(repo, p, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if report.Healthy {
		t.Fatal("tampered backup accepted")
	}
}
func TestApplyDeletesVerifiedBackup(t *testing.T) {
	root := t.TempDir()
	repo, e := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer repo.Close()
	backupRoot := filepath.Join(root, "backups")
	if e = os.MkdirAll(backupRoot, 0700); e != nil {
		t.Fatal(e)
	}
	b, e := repo.Backup(filepath.Join(backupRoot, "old.db"))
	if e != nil {
		t.Fatal(e)
	}
	p := Policy{Version: Version, BackupRoot: backupRoot, ApplyEnabled: true, KeepLatest: 0, MinimumAgeHours: 0}
	report, e := Evaluate(repo, p, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Apply(repo, report, "wrong", time.Now()); e == nil {
		t.Fatal("wrong confirmation accepted")
	}
	result, e := Apply(repo, report, Confirmation, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if result.Deleted != 1 {
		t.Fatalf("deleted=%d", result.Deleted)
	}
	if _, e = os.Stat(b.Path); !os.IsNotExist(e) {
		t.Fatal("backup file remains")
	}
	records, e := repo.Backups()
	if e != nil {
		t.Fatal(e)
	}
	if len(records) != 0 {
		t.Fatal("backup record remains")
	}
}
