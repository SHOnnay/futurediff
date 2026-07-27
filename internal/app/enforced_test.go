package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

const appTestImage = "example.invalid/futurediff/runtime@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func TestEnforcedTransactionExecutesInOCIAndPersistsEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake OCI runtime uses POSIX shell")
	}
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
	runner := &runtimeoci.Runner{Kind: runtimeoci.Docker, Binary: writeAppFakeDocker(t), Policy: runtimeoci.DefaultPolicy(appTestImage), ScratchRoot: filepath.Join(tmp, "scratch")}
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, OCI: runner, Verifier: verification.Engine{OCI: runner}}

	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "enforced", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.Execute(context.Background(), created.Transaction.ID, ExecuteRequest{Command: []string{"/bin/sh", "-c", "test ! -e .git; printf 'future\\n' > README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Execution.WorkspaceSynchronized || view.Execution.RuntimeKind != "docker" {
		t.Fatalf("unexpected execution: %+v", view.Execution)
	}
	live, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if string(live) != "current\n" {
		t.Fatalf("live checkout changed: %q", live)
	}
	workspaceBytes, _ := os.ReadFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"))
	if string(workspaceBytes) != "future\n" {
		t.Fatalf("workspace not updated: %q", workspaceBytes)
	}
	if _, err := os.Stat(view.Execution.EvidencePath); err != nil {
		t.Fatal(err)
	}
	executions, err := store.RuntimeExecutions(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].ExecutionID != view.Execution.ExecutionID {
		t.Fatalf("execution ledger mismatch: %+v", executions)
	}

	sealed, err := svc.Seal(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "oci-check", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "oci_command", Type: "command", Command: []string{"/bin/sh", "-c", "grep -q future README.md"}}}}
	verified, err := svc.Verify(created.Transaction.ID, contract)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Transaction.Status != domain.StateReady {
		t.Fatalf("expected ready, got %s", verified.Transaction.Status)
	}
	if sealed.Patch == nil || sealed.Patch.PatchSHA256 == "" {
		t.Fatal("missing patch")
	}
	workspaceBytes, _ = os.ReadFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"))
	if string(workspaceBytes) != "future\n" {
		t.Fatalf("verification changed workspace: %q", workspaceBytes)
	}
}

func TestEnforcedModeRejectedWithoutRuntime(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}}
	if _, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "enforced"}); err == nil {
		t.Fatal("enforced transaction created without runtime")
	}
}

func writeAppFakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-docker")
	state := filepath.Join(dir, "image-present")
	if err := os.WriteFile(state, []byte("present\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
state="` + state + `"
case "$1" in
  version) echo "fake-1.0" ;;
  info) echo '["name=rootless"]' ;;
  image)
    shift
    [ "$1" = inspect ] || exit 125
    shift
    [ -f "$state" ] || exit 1
    echo '[{"Id":"sha256:test"}]'
    ;;
  pull)
    shift
    : > "$state"
    echo "pulled $1"
    ;;
  run)
    shift
    src=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --rm|--init|--pull=never|--read-only) shift ;;
        --network|--cap-drop|--security-opt|--pids-limit|--memory|--cpus|--tmpfs|--workdir|--user|--userns|--env) shift 2 ;;
        --mount) src=$(printf '%s' "$2" | sed -n 's/.*src=\([^,]*\).*/\1/p'); shift 2 ;;
        *@sha256:*) [ -f "$state" ] || exit 125; shift; break ;;
        *) exit 125 ;;
      esac
    done
    cd "$src"
    exec "$@"
    ;;
  *) exit 125 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
