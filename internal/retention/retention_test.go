package retention

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
)

func TestBuildAndApplyRetentionPlan(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, source, "init")
	git(t, source, "config", "user.email", "test@example.com")
	git(t, source, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, source, "add", "README.md")
	git(t, source, "commit", "-m", "initial")
	manager := staging.Manager{RuntimeRoot: filepath.Join(root, "runtime")}
	inspected, err := manager.Inspect(source, staging.Reject)
	if err != nil {
		t.Fatal(err)
	}
	txID := domain.NewID("tx")
	workspace, err := manager.Create(txID, inspected, staging.Reject)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.ArtifactsPath, "evidence.txt"), []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if _, err = repo.Create(ledger.CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Transition(txID, domain.StateActive, domain.StateAborting, "test", "done", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Transition(txID, domain.StateAborting, domain.StateAborted, "test", "done", false, false); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(repo, root, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 || plan.TotalBytes == 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if _, err = Apply(repo, plan, "wrong"); err == nil {
		t.Fatal("expected confirmation failure")
	}
	result, err := Apply(repo, plan, Confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.BytesRemoved == 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(filepath.Dir(workspace.WorkspacePath)); !os.IsNotExist(err) {
		t.Fatalf("runtime root still exists: %v", err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
