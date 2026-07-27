package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRecoveryFinalizesPublishedRef(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")

	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}}
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealed, err := svc.Seal(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(created.Transaction.ID, digest, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginCommit(created.Transaction.ID, digest); err != nil {
		t.Fatal(err)
	}
	patch := *sealed.Patch
	if _, err := svc.Staging.Materialize(created.Workspace, patch, digest); err != nil {
		t.Fatal(err)
	}

	recovered, err := svc.Recover(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateCommitted {
		t.Fatalf("expected committed, got %s", recovered.Transaction.Status)
	}
}
