package runtimeoci

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testImage = "example.invalid/futurediff/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestPinnedImageRequired(t *testing.T) {
	_, err := BuildPlan(Backend{Kind: Docker, Binary: "docker", Rootless: true}, "/x", []string{"true"}, DefaultPolicy("alpine:latest"))
	if err == nil {
		t.Fatal("tag-only image accepted")
	}
}

func TestBuildPlanContainsEnforcedControls(t *testing.T) {
	plan, err := BuildPlan(Backend{Kind: Docker, Binary: "/usr/bin/docker", Version: "test", Rootless: true}, t.TempDir(), []string{"/bin/sh", "-c", "true"}, DefaultPolicy(testImage))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Args, " ")
	for _, expected := range []string{"--pull=never", "--read-only", "--network none", "--cap-drop ALL", "--security-opt no-new-privileges", "--pids-limit 256", "--memory 2g", "--cpus 2.0", "--user 0:0", testImage} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %s", expected, joined)
		}
	}
	if strings.Contains(joined, ",dst=/workspace,rw") {
		t.Fatalf("docker mount syntax still uses invalid rw flag: %s", joined)
	}
}

func TestPolicyRejectsWeakenedBoundary(t *testing.T) {
	p := DefaultPolicy(testImage)
	p.Network = "bridge"
	if err := p.Validate(); err == nil {
		t.Fatal("networked enforced policy accepted")
	}
	p = DefaultPolicy(testImage)
	p.RequireRootless = false
	if err := p.Validate(); err == nil {
		t.Fatal("non-rootless enforced policy accepted")
	}
}

func TestSensitiveEnvironmentRejected(t *testing.T) {
	if err := validateEnvironment(map[string]string{"GITHUB_TOKEN": "secret"}); err == nil {
		t.Fatal("sensitive environment accepted")
	}
	if err := validateEnvironment(map[string]string{"CI": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizedCopyRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := copySanitized(source, filepath.Join(t.TempDir(), "copy"), DefaultPolicy(testImage)); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestRunnerSynchronizesSuccessfulMutationWithoutGitMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX shell")
	}
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".git"), "gitdir: protected\n")
	mustWrite(t, filepath.Join(workspace, "tracked.txt"), "before\n")
	mustWrite(t, filepath.Join(workspace, "delete.txt"), "delete\n")
	runner := Runner{Kind: Docker, Binary: fakeDocker(t, true), Policy: DefaultPolicy(testImage), ScratchRoot: t.TempDir()}
	result, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "exec", Workspace: workspace, Command: []string{"/bin/sh", "-c", "test ! -e .git; printf 'after\\n' > tracked.txt; rm delete.txt; mkdir generated; printf 'new\\n' > generated/file.txt"}, Purpose: Mutation, SyncWorkspace: true})
	if err != nil {
		t.Fatalf("run: %v stdout=%s stderr=%s", err, result.Stdout, result.Stderr)
	}
	if !result.Evidence.WorkspaceSynchronized {
		t.Fatal("workspace was not synchronized")
	}
	assertContent(t, filepath.Join(workspace, ".git"), "gitdir: protected\n")
	assertContent(t, filepath.Join(workspace, "tracked.txt"), "after\n")
	assertContent(t, filepath.Join(workspace, "generated", "file.txt"), "new\n")
	if _, err := os.Stat(filepath.Join(workspace, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete was not synchronized: %v", err)
	}
}

func TestFailedMutationDoesNotSynchronize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX shell")
	}
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "tracked.txt"), "before\n")
	runner := Runner{Kind: Docker, Binary: fakeDocker(t, true), Policy: DefaultPolicy(testImage), ScratchRoot: t.TempDir()}
	result, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "exec", Workspace: workspace, Command: []string{"/bin/sh", "-c", "printf 'unsafe\\n' > tracked.txt; exit 7"}, Purpose: Mutation, SyncWorkspace: true})
	if err == nil {
		t.Fatal("failed command returned nil error")
	}
	if result.ExitCode != 7 || result.Evidence.WorkspaceSynchronized {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertContent(t, filepath.Join(workspace, "tracked.txt"), "before\n")
}

func TestVerificationNeverSynchronizes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX shell")
	}
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "tracked.txt"), "before\n")
	runner := Runner{Kind: Docker, Binary: fakeDocker(t, true), Policy: DefaultPolicy(testImage), ScratchRoot: t.TempDir()}
	result, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "verify", Workspace: workspace, Command: []string{"/bin/sh", "-c", "printf 'side-effect\\n' > tracked.txt"}, Purpose: Verification, SyncWorkspace: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.Evidence.WorkspaceSynchronized {
		t.Fatal("verification synchronized")
	}
	assertContent(t, filepath.Join(workspace, "tracked.txt"), "before\n")
}

func TestOutputIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX shell")
	}
	p := DefaultPolicy(testImage)
	p.MaxOutputBytes = 32
	p.Timeout = 2 * time.Second
	runner := Runner{Kind: Docker, Binary: fakeDocker(t, true), Policy: p, ScratchRoot: t.TempDir()}
	result, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "out", Workspace: t.TempDir(), Command: []string{"/bin/sh", "-c", "printf 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'"}, Purpose: Verification})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stdout) != 32 || !result.Evidence.StdoutTruncated || result.Evidence.StdoutBytes <= 32 {
		t.Fatalf("output not bounded: %+v len=%d", result.Evidence, len(result.Stdout))
	}
}

func TestNonRootlessRuntimeRejected(t *testing.T) {
	runner := Runner{Kind: Docker, Binary: "unused", Policy: DefaultPolicy(testImage), ProbeIdentity: func(context.Context, RuntimeKind, string) (Backend, error) {
		return Backend{Kind: Docker, Binary: "/bin/false", Version: "test", Rootless: false}, nil
	}}
	_, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "exec", Workspace: t.TempDir(), Command: []string{"true"}, Purpose: Mutation, SyncWorkspace: true})
	if err == nil || !strings.Contains(err.Error(), "rootless") {
		t.Fatalf("expected rootless rejection, got %v", err)
	}
}

func TestRunnerPullsMissingImageBeforeExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake runtime uses POSIX shell")
	}
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "tracked.txt"), "before\n")
	runner := Runner{Kind: Docker, Binary: fakeDocker(t, false), Policy: DefaultPolicy(testImage), ScratchRoot: t.TempDir()}
	result, err := runner.Execute(context.Background(), Request{TransactionID: "tx", ExecutionID: "pull", Workspace: workspace, Command: []string{"/bin/sh", "-c", "printf 'after\\n' > tracked.txt"}, Purpose: Mutation, SyncWorkspace: true})
	if err != nil {
		t.Fatalf("run: %v stdout=%s stderr=%s", err, result.Stdout, result.Stderr)
	}
	assertContent(t, filepath.Join(workspace, "tracked.txt"), "after\n")
}

func fakeDocker(t *testing.T, imagePresent bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-docker")
	state := filepath.Join(dir, "image-present")
	if imagePresent {
		mustWrite(t, state, "present\n")
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
        *) echo "unexpected argument: $1" >&2; exit 125 ;;
      esac
    done
    [ -n "$src" ] || exit 125
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

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s: got %q want %q", path, got, want)
	}
}

func TestMinimalRuntimeEnvironmentDoesNotInheritAmbientSecrets(t *testing.T) {
	t.Setenv("FUTUREDIFF_GITHUB_TOKEN", "must-not-reach-runtime-probe")
	for _, entry := range minimalRuntimeEnv() {
		if strings.HasPrefix(entry, "FUTUREDIFF_GITHUB_TOKEN=") || strings.Contains(entry, "must-not-reach-runtime-probe") {
			t.Fatalf("ambient secret inherited by runtime subprocess: %q", entry)
		}
	}
}
