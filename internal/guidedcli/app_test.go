package guidedcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type fakeEngine struct {
	state     string
	approved  bool
	workspace string
	repo      string
	calls     [][]string
}

func (f *fakeEngine) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	id := "tx_test"
	response := func() []byte {
		value := Response{
			Transaction: &Transaction{TransactionID: id, Status: f.state, Mode: "cooperative"},
			Workspace:   &Workspace{TransactionID: id, RepositoryRoot: f.repo, WorkspacePath: f.workspace},
			Patch:       &Patch{TransactionID: id, ChangedPaths: []string{"README.md"}, PatchSHA256: strings.Repeat("a", 64), PatchSizeBytes: 12},
		}
		if f.approved {
			value.Transaction.ApprovalDigest = strings.Repeat("d", 64)
		}
		data, _ := json.Marshal(value)
		return data
	}
	switch args[0] {
	case "health":
		return []byte(`{"status":"ok"}`), nil
	case "list":
		data, _ := json.Marshal(Response{Transactions: []Transaction{{TransactionID: id, Status: f.state, WorkspaceIdentity: f.workspace}}})
		return data, nil
	case "get":
		return response(), nil
	case "seal":
		f.state = "sealed"
		return response(), nil
	case "verify":
		if len(args) != 3 {
			return nil, fmt.Errorf("verify args: %v", args)
		}
		data, err := os.ReadFile(args[2])
		if err != nil || !bytes.Contains(data, []byte("basic-repository-check")) {
			return nil, fmt.Errorf("default policy was not materialized")
		}
		f.state = "ready"
		return response(), nil
	case "approval-material":
		return []byte(`{"transaction_id":"tx_test","transaction_digest":"` + strings.Repeat("d", 64) + `"}`), nil
	case "approve":
		if len(args) != 3 || args[2] != strings.Repeat("d", 64) {
			return nil, fmt.Errorf("wrong approval digest: %v", args)
		}
		f.approved = true
		return response(), nil
	case "commit":
		if !f.approved {
			return nil, fmt.Errorf("commit before approval")
		}
		if len(args) != 3 || args[2] != strings.Repeat("d", 64) {
			return nil, fmt.Errorf("wrong commit digest: %v", args)
		}
		f.state = "committed"
		return response(), nil
	case "abort":
		f.state = "aborted"
		return response(), nil
	default:
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
}

func newTestApp(t *testing.T, engine Engine, repo, workspace string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	store := StateStore{Path: filepath.Join(realTempDir(t), "current.json")}
	if err := store.Save("tx_test", repo); err != nil {
		t.Fatal(err)
	}
	renderer := Renderer{Out: out, Err: errOut, Color: false, Unicode: false}
	app := &App{
		In: strings.NewReader(""), Out: out, Err: errOut,
		Engine: engine, Store: store, Renderer: renderer,
		Binary: "futurediff", DaemonBinary: "futurediffd",
		JSON: false, Yes: true, Interactive: false, GitBinary: "git",
	}
	app.Daemon = DaemonManager{Engine: engine, Binary: "futurediffd"}
	return app, out, errOut
}

func makeRepoAndWorkspace(t *testing.T) (string, string) {
	t.Helper()
	repo := realTempDir(t)
	runGitTest(t, repo, "init", "-b", "main")
	runGitTest(t, repo, "config", "user.name", "Test")
	runGitTest(t, repo, "config", "user.email", "test@localhost")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "README.md")
	runGitTest(t, repo, "commit", "-m", "init")
	workspace := filepath.Join(realTempDir(t), "workspace")
	runGitTest(t, repo, "worktree", "add", "--detach", workspace, "HEAD")
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("hello\nfuture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, workspace
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestFinishResolvesDigestAndCompletesStateMachine(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"finish", "tx_test"}); code != 0 {
		t.Fatalf("finish failed: %s", out.String())
	}
	if engine.state != "committed" || !engine.approved {
		t.Fatalf("final fake state = %s approved=%v", engine.state, engine.approved)
	}
	var names []string
	for _, call := range engine.calls {
		names = append(names, call[0])
	}
	for _, required := range []string{"seal", "verify", "approval-material", "approve", "commit"} {
		found := false
		for _, name := range names {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s call in %v", required, names)
		}
	}
}

func TestApproveUsesTransactionDigestNotPatchDigest(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", repo: repo, workspace: workspace}
	app, _, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"approve", "tx_test"}); code != 0 {
		t.Fatalf("approve exit code %d", code)
	}
	last := engine.calls[len(engine.calls)-1]
	want := []string{"approve", "tx_test", strings.Repeat("d", 64)}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("approve call = %v, want %v", last, want)
	}
}

func TestApproveRefusesNonInteractiveWithoutYes(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", repo: repo, workspace: workspace}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	app.Yes = false
	if code := app.Run(context.Background(), []string{"approve", "tx_test"}); code == 0 {
		t.Fatal("approval unexpectedly succeeded")
	}
	if !strings.Contains(errOut.String(), "explicit --yes") {
		t.Fatalf("unexpected error: %s", errOut.String())
	}
}

func TestStatusJSONContainsNoANSI(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	app.JSON = true
	app.Renderer.Color = false
	if code := app.Run(context.Background(), []string{"status", "tx_test"}); code != 0 {
		t.Fatalf("status exit code %d", code)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("JSON output contains ANSI")
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
}

func TestCompletionScriptsAreGenerated(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatalf("%s completion: %v", shell, err)
		}
		if !strings.Contains(script, "fdif") || !strings.Contains(script, "finish") {
			t.Fatalf("%s completion is incomplete: %s", shell, script)
		}
	}
}

func TestFinishJSONProducesSingleDocument(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	app.JSON = true
	if code := app.Run(context.Background(), []string{"finish", "tx_test"}); code != 0 {
		t.Fatalf("finish exit code %d: %s", code, out.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("finish did not produce one JSON document: %v\n%s", err, out.String())
	}
	if decoded["kind"] != "fdif-publish" {
		t.Fatalf("unexpected final JSON: %v", decoded)
	}
}

type demoEngine struct {
	t         *testing.T
	state     string
	approved  bool
	repo      string
	workspace string
}

func (d *demoEngine) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	const id = "tx_demo"
	response := func() []byte {
		value := Response{
			Transaction: &Transaction{TransactionID: id, Status: d.state, Mode: "cooperative"},
			Workspace:   &Workspace{TransactionID: id, RepositoryRoot: d.repo, WorkspacePath: d.workspace},
			Patch:       &Patch{TransactionID: id, ChangedPaths: []string{"README.md"}, PatchSHA256: strings.Repeat("a", 64), PatchSizeBytes: 12},
		}
		if d.approved {
			value.Transaction.ApprovalDigest = strings.Repeat("d", 64)
		}
		data, _ := json.Marshal(value)
		return data
	}
	switch args[0] {
	case "health":
		return []byte(`{"status":"ok"}`), nil
	case "create":
		d.repo = args[1]
		d.workspace = filepath.Join(d.t.TempDir(), "workspace")
		runGitTest(d.t, d.repo, "worktree", "add", "--detach", d.workspace, "HEAD")
		d.state = "active"
		return response(), nil
	case "get":
		return response(), nil
	case "list":
		data, _ := json.Marshal(Response{Transactions: []Transaction{{TransactionID: id, Status: d.state, WorkspaceIdentity: d.workspace}}})
		return data, nil
	case "seal":
		d.state = "sealed"
		return response(), nil
	case "verify":
		d.state = "ready"
		return response(), nil
	case "approval-material":
		return []byte(`{"transaction_id":"tx_demo","transaction_digest":"` + strings.Repeat("d", 64) + `"}`), nil
	case "approve":
		d.approved = true
		return response(), nil
	case "commit":
		if !d.approved {
			return nil, fmt.Errorf("not approved")
		}
		runGitTest(d.t, d.workspace, "add", "--all")
		runGitTest(d.t, d.workspace, "-c", "user.name=FutureDiff Test", "-c", "user.email=test@localhost", "commit", "-m", "publish demo")
		commit := strings.TrimSpace(gitOutputForTest(d.t, d.workspace, "rev-parse", "HEAD"))
		runGitTest(d.t, d.repo, "branch", "futurediff/"+id, commit)
		d.state = "committed"
		return response(), nil
	default:
		return nil, fmt.Errorf("unexpected demo command: %v", args)
	}
}

func gitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func TestDemoProvesCurrentBranchIsolationAndPublishedBranch(t *testing.T) {
	engine := &demoEngine{t: t}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	store := StateStore{Path: filepath.Join(realTempDir(t), "current.json")}
	app := &App{
		In: strings.NewReader(""), Out: out, Err: errOut,
		Engine: engine, Store: store,
		Renderer: Renderer{Out: out, Err: errOut, Color: false, Unicode: false},
		JSON:     true, Yes: true, Interactive: false, GitBinary: "git",
	}
	app.Daemon = DaemonManager{Engine: engine, Binary: "futurediffd"}
	if code := app.Run(context.Background(), []string{"demo", "--yes"}); code != 0 {
		t.Fatalf("demo exit code %d: stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("demo JSON: %v\n%s", err, out.String())
	}
	if result["current_branch_unchanged"] != true || result["published_branch_verified"] != true {
		t.Fatalf("demo did not prove both boundaries: %v", result)
	}
}

func TestReviewIncludesStagedChanges(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, workspace, "add", "README.md")
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"review", "tx_test"}); code != 0 {
		t.Fatalf("review exit code %d", code)
	}
	if !strings.Contains(out.String(), "1 insertion") {
		t.Fatalf("staged change missing from review summary:\n%s", out.String())
	}
}

func TestPublishClearsCurrentPointerAndExplainsBranch(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", approved: true, repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"publish", "tx_test"}); code != 0 {
		t.Fatalf("publish exit code %d", code)
	}
	if _, err := app.Store.Load(); !os.IsNotExist(err) {
		t.Fatalf("current pointer was not cleared: %v", err)
	}
	if !strings.Contains(out.String(), "futurediff/tx_test") || !strings.Contains(out.String(), "unchanged by FutureDiff") {
		t.Fatalf("publish output did not explain safe branch behavior:\n%s", out.String())
	}
}

func TestResolveExecutableRejectsMissingAbsolutePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-futurediff")
	if _, err := resolveExecutable(missing); err == nil {
		t.Fatal("missing absolute executable was accepted")
	}
}

func TestRendererDisablesDecorationForNonTerminalWriter(t *testing.T) {
	out := &bytes.Buffer{}
	renderer := NewRenderer(out, out, false)
	if renderer.Color || renderer.Unicode {
		t.Fatalf("non-terminal renderer enabled decoration: %+v", renderer)
	}
}

func TestResolveExecutableRejectsNonExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use POSIX executable bits")
	}
	path := filepath.Join(t.TempDir(), "futurediff")
	if err := os.WriteFile(path, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveExecutable(path); err == nil {
		t.Fatal("non-executable file was accepted")
	}
}
