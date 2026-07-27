package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/quota"
	"github.com/SHOnnay/futurediff/internal/staging"
)

func TestOpenTransactionQuota(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := quota.Default()
	p.MaxOpenTransactions = 1
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(root, "runtime")}, Quotas: p}
	if _, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative"}); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected quota error, got %v", err)
	}
}

func TestPatchQuotaRejectsBeforeSeal(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := quota.Default()
	p.MaxPatchBytes = 8
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(root, "runtime")}, Quotas: p}
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte(strings.Repeat("future", 50)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err == nil || !strings.Contains(err.Error(), "exceeds quota") {
		t.Fatalf("expected patch quota error, got %v", err)
	}
}
