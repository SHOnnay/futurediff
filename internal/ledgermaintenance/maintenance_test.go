package ledgermaintenance

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceNeedsConfirmation(t *testing.T) {
	root := t.TempDir()
	r, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := Run(root, true, "bad", time.Now()); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	rep, err := Run(root, true, Confirmation, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Applied || rep.Backup.SHA256 == "" {
		t.Fatalf("%+v", rep)
	}
}
