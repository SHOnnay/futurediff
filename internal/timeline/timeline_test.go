package timeline

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

func TestBuildMarkdownMermaid(t *testing.T) {
	root := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	tx := domain.Transaction{ID: "tx_timeline", Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}
	ws := domain.Workspace{TransactionID: tx.ID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "w"), ArtifactsPath: filepath.Join(root, "a"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}
	if _, err := repo.Create(ledger.CreateInput{Transaction: tx, Workspace: ws}); err != nil {
		t.Fatal(err)
	}
	r, err := Build(repo, tx.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) == 0 || r.Digest == "" {
		t.Fatalf("report=%+v", r)
	}
	if !strings.Contains(Markdown(r), "transaction timeline") || !strings.Contains(Mermaid(r), "flowchart TD") {
		t.Fatal("render missing")
	}
}
