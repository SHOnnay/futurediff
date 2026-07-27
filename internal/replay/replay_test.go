package replay

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"testing"
	"time"
)

func TestTransactionReplay(t *testing.T) {
	root := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	id := "tx_replay"
	_, err = repo.Create(ledger.CreateInput{Transaction: domain.Transaction{ID: id, Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}, Workspace: domain.Workspace{TransactionID: id, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "w"), ArtifactsPath: filepath.Join(root, "a"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Transition(id, domain.StateActive, domain.StateAborting, "test", "abort", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Transition(id, domain.StateAborting, domain.StateAborted, "test", "done", false, true); err != nil {
		t.Fatal(err)
	}
	report, err := Transaction(repo, id)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.ReplayedStatus != domain.StateAborted {
		t.Fatalf("report=%+v", report)
	}
}
