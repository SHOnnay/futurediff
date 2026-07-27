package leasecleanup

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlyExpiredDeleted(t *testing.T) {
	root := t.TempDir()
	r, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := r.AcquireLease("old", "o", time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AcquireLease("new", "o", time.Hour); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	r.Close()
	rep, err := Run(root, true, Confirmation, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted != 1 {
		t.Fatalf("%+v", rep)
	}
}
