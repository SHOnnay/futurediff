package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

func TestCreateAndCASState(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().UTC()
	tx, err := r.Create(CreateInput{Transaction: domain.Transaction{ID: "tx1", Mode: "cooperative", PolicyVersion: "p", CreatedAt: now}, Workspace: domain.Workspace{TransactionID: "tx1", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"}})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != domain.StateActive || tx.Revision != 1 {
		t.Fatalf("unexpected transaction: %+v", tx)
	}
	if _, err := r.Transition("tx1", domain.StateActive, domain.StateSealed, "test", "seal", true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Transition("tx1", domain.StateActive, domain.StateAborting, "test", "stale expectation", false, true); err == nil {
		t.Fatal("expected state conflict")
	}
}
