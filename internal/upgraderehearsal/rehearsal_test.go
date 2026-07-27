package upgraderehearsal

import (
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	repo.Close()
	r, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !r.RehearsalSucceeded || !r.SourceUnchanged || r.AfterMigrationCount == 0 {
		t.Fatal("rehearsal failed")
	}
}
