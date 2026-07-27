package ledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failAt map[string]int

func (f failAt) Before(op string) error {
	if f[op] > 0 {
		f[op]--
		return errors.New("injected " + op + " failure")
	}
	return nil
}

func TestCommitFailureRollsBackTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.db")
	faults := failAt{}
	db, err := OpenWithFaultInjector(path, faults)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ExecScript("CREATE TABLE items(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	faults["commit"] = 1
	err = db.WithTx(func(tx *Tx) error { _, e := tx.Exec("INSERT INTO items(value) VALUES(?)", "should-rollback"); return e })
	if err == nil {
		t.Fatal("expected injected commit failure")
	}
	rows, err := db.Query("SELECT COUNT(*) AS count FROM items")
	if err != nil {
		t.Fatal(err)
	}
	if Int64(rows[0], "count") != 0 {
		t.Fatalf("commit failure leaked data: %#v", rows)
	}
}

func TestBackupFailureDoesNotReplaceExistingArtifact(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.db")
	faults := failAt{}
	db, err := OpenWithFaultInjector(path, faults)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	target := filepath.Join(root, "backup.db")
	if err := os.WriteFile(target, []byte("known-good"), 0o600); err != nil {
		t.Fatal(err)
	}
	faults["backup"] = 1
	if err := db.BackupTo(target + ".tmp"); err == nil {
		t.Fatal("expected backup failure")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "known-good" {
		t.Fatalf("existing artifact changed: %q", got)
	}
}

func TestIntegrityCheckDetectsCorruptedBackup(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ExecScript("CREATE TABLE items(id INTEGER PRIMARY KEY); INSERT INTO items DEFAULT VALUES;"); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.db")
	if err := db.BackupTo(backup); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 512 {
		t.Fatal("backup unexpectedly small")
	}
	for i := 100; i < 140; i++ {
		data[i] ^= 0xff
	}
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		t.Fatal(err)
	}
	corrupted, err := Open(backup)
	if err == nil {
		defer corrupted.Close()
		if err := corrupted.IntegrityCheck(); err == nil {
			t.Fatal("expected corrupted backup rejection")
		}
	}
}
