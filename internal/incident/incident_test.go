package incident

import (
	"github.com/SHOnnay/futurediff/internal/demo"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildCommittedIncidentReport(t *testing.T) {
	root := t.TempDir()
	d, err := demo.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := ledger.OpenRepository(filepath.Join(root, "state", "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	r, err := Build(repo, d.TransactionID, time.Unix(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.Severity != "informational" || !r.Replay.Valid || r.Digest == "" {
		t.Fatalf("report=%+v", r)
	}
	if !strings.Contains(Markdown(r), "Incident Reconstruction") {
		t.Fatal("markdown missing")
	}
}
