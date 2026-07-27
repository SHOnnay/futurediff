package api

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
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

func TestUnixSocketLifecycle(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "add", ".")
	gitCmd(t, repoPath, "commit", "-m", "base")

	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &app.Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}}
	socket := shortSocketPath(t, "fd-api-")

	server := &Server{Service: svc, SocketPath: socket}
	go func() { _ = server.Serve() }()
	defer server.Close()
	client := NewClient(socket)
	for i := 0; i < 100; i++ {
		if _, err := client.Do("GET", "/v1/health", nil); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	createdRaw, err := client.Do("POST", "/v1/transactions", app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var created app.TransactionView
	if err := json.Unmarshal(createdRaw, &created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := created.Transaction.ID
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/seal", nil); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/verify", contract); err != nil {
		t.Fatal(err)
	}
	materialRaw, err := client.Do("GET", "/v1/transactions/"+id+"/approval-material", nil)
	if err != nil {
		t.Fatal(err)
	}
	var material map[string]string
	if err := json.Unmarshal(materialRaw, &material); err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/approve", map[string]string{"transaction_digest": digest, "approver": "test"}); err != nil {
		t.Fatal(err)
	}
	committedRaw, err := client.Do("POST", "/v1/transactions/"+id+"/commit", map[string]string{"transaction_digest": digest})
	if err != nil {
		t.Fatal(err)
	}
	var committed app.TransactionView
	if err := json.Unmarshal(committedRaw, &committed); err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status %s", committed.Transaction.Status)
	}
	live, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if string(live) != "current\n" {
		t.Fatalf("live checkout changed: %q", live)
	}
	future := gitCmd(t, repoPath, "show", "refs/heads/futurediff/"+id+":README.md")
	if future != "future" {
		t.Fatalf("future ref mismatch: %q", future)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %o", info.Mode().Perm())
	}
}

func TestUnixSocketEnforcedOCIExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake OCI runtime uses POSIX shell")
	}
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo-enforced")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "add", ".")
	gitCmd(t, repoPath, "commit", "-m", "base")

	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger-enforced.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &runtimeoci.Runner{Kind: runtimeoci.Docker, Binary: writeAPIFakeDocker(t), Policy: runtimeoci.DefaultPolicy("example.invalid/futurediff/runtime@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"), ScratchRoot: filepath.Join(tmp, "scratch")}
	svc := &app.Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime-enforced")}, Verifier: verification.Engine{OCI: runner}, OCI: runner}
	socket := shortSocketPath(t, "fd-api-enforced-")
	server := &Server{Service: svc, SocketPath: socket}
	go func() { _ = server.Serve() }()
	defer server.Close()
	client := NewClient(socket)
	for i := 0; i < 100; i++ {
		if _, err := client.Do("GET", "/v1/health", nil); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	healthRaw, err := client.Do("GET", "/v1/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	if err := json.Unmarshal(healthRaw, &health); err != nil {
		t.Fatal(err)
	}
	ociStatus, ok := health["oci"].(map[string]any)
	if !ok || ociStatus["enforced_ready"] != true {
		t.Fatalf("unexpected health: %s", healthRaw)
	}

	createdRaw, err := client.Do("POST", "/v1/transactions", app.CreateRequest{Repository: repoPath, Mode: "enforced", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var created app.TransactionView
	if err := json.Unmarshal(createdRaw, &created); err != nil {
		t.Fatal(err)
	}
	executedRaw, err := client.Do("POST", "/v1/transactions/"+created.Transaction.ID+"/execute", app.ExecuteRequest{Command: []string{"/bin/sh", "-c", "test ! -e .git; printf 'future\\n' > README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	var executed app.ExecuteView
	if err := json.Unmarshal(executedRaw, &executed); err != nil {
		t.Fatal(err)
	}
	if !executed.Execution.WorkspaceSynchronized {
		t.Fatalf("execution was not synchronized: %+v", executed)
	}
	live, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if string(live) != "current\n" {
		t.Fatalf("live checkout changed: %q", live)
	}
	staged, _ := os.ReadFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"))
	if string(staged) != "future\n" {
		t.Fatalf("staged workspace mismatch: %q", staged)
	}
}

func writeAPIFakeDocker(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-docker")
	script := `#!/bin/sh
set -eu
case "$1" in
  version) echo "fake-1.0" ;;
  info) echo '["name=rootless"]' ;;
  run)
    shift
    src=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --rm|--init|--pull=never|--read-only) shift ;;
        --network|--cap-drop|--security-opt|--pids-limit|--memory|--cpus|--tmpfs|--workdir|--user|--userns|--env) shift 2 ;;
        --mount) src=$(printf '%s' "$2" | sed -n 's/.*src=\([^,]*\).*/\1/p'); shift 2 ;;
        *@sha256:*) shift; break ;;
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
