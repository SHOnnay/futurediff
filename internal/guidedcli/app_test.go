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
	effects   []ExternalEffect
	receipts  []EffectReceipt
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
			Workspace:   &Workspace{TransactionID: id, RepositoryRoot: f.repo, WorkspacePath: f.workspace, SourceHeadRef: "refs/heads/main"},
			Patch:       &Patch{TransactionID: id, ChangedPaths: []string{"README.md"}, PatchSHA256: strings.Repeat("a", 64), PatchSizeBytes: 12},
			Effects:     append([]ExternalEffect(nil), f.effects...),
			Receipts:    append([]EffectReceipt(nil), f.receipts...),
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
	case "create":
		if len(args) >= 2 {
			f.repo = args[1]
		}
		if f.state == "" {
			f.state = "active"
		}
		return response(), nil
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
	case "prepare-github-branch":
		if f.state != "sealed" || len(args) != 7 {
			return nil, fmt.Errorf("prepare branch args/state: %v %s", args, f.state)
		}
		input, _ := json.Marshal(githubBranchInput{Owner: args[3], Repo: args[4], Branch: args[5], RemoteURL: args[6]})
		effect := ExternalEffect{EffectID: "effect_branch", TransactionID: id, AdapterIdentity: githubBranchAdapterID, CredentialID: args[2], InputJSON: string(input), Status: "verified"}
		f.effects = append(f.effects, effect)
		return json.Marshal(effect)
	case "prepare-github-pr":
		if f.state != "sealed" || len(args) != 10 || args[9] != "effect_branch" {
			return nil, fmt.Errorf("prepare PR args/state: %v %s", args, f.state)
		}
		input, _ := json.Marshal(githubDraftInput{Owner: args[3], Repo: args[4], Head: args[5], Base: args[6], Title: args[7], Body: args[8], DependsOnEffectID: args[9]})
		effect := ExternalEffect{EffectID: "effect_pr", TransactionID: id, AdapterIdentity: githubDraftAdapterID, CredentialID: args[2], InputJSON: string(input), Status: "verified", DependsOn: []string{"effect_branch"}}
		f.effects = append(f.effects, effect)
		return json.Marshal(effect)
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
		if len(f.effects) > 0 {
			f.receipts = []EffectReceipt{
				{ReceiptID: "receipt_branch", EffectID: "effect_branch", ProviderResourceID: "github://octo/example/refs/heads/futurediff/tx_test"},
				{ReceiptID: "receipt_pr", EffectID: "effect_pr", ProviderResourceID: "github://octo/example/pulls/42"},
			}
		}
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
	if !strings.Contains(out.String(), "futurediff/tx_test") || !strings.Contains(out.String(), "Current branch") || !strings.Contains(out.String(), "unchanged") {
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

func TestNewAliasCreatesSafeWorkingCopy(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"new", repo}); code != 0 {
		t.Fatalf("new exit code %d", code)
	}
	if len(engine.calls) == 0 || engine.calls[len(engine.calls)-1][0] != "create" {
		t.Fatalf("new did not dispatch create: %v", engine.calls)
	}
	for _, phrase := range []string{"Safe working copy created", "Work only in the safe working copy", "editor or coding agent"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("start output missing %q:\n%s", phrase, out.String())
		}
	}
}

func TestDiscardAliasAbortsWithFriendlyOutput(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"discard", "tx_test"}); code != 0 {
		t.Fatalf("discard exit code %d", code)
	}
	if engine.state != "aborted" {
		t.Fatalf("discard state = %s", engine.state)
	}
	if !strings.Contains(out.String(), "Change discarded") {
		t.Fatalf("discard output was not friendly:\n%s", out.String())
	}
}

func TestHelpSeparatesEverydayAndAdvancedWorkflow(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("help exit code %d", code)
	}
	for _, phrase := range []string{"Everyday workflow", "Advanced workflow", "cooperative mode is the public-alpha default", "fdif start|new", "fdif abort|discard"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("help missing %q:\n%s", phrase, out.String())
		}
	}
}

func TestCompletionIncludesFriendlyAliases(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatalf("%s completion: %v", shell, err)
		}
		for _, alias := range []string{"new", "discard"} {
			if !strings.Contains(script, alias) {
				t.Fatalf("%s completion missing %s: %s", shell, alias, script)
			}
		}
	}
}

func TestCommittedCompletionFilesMatchGenerated(t *testing.T) {
	cases := map[string]string{
		"bash":       "fdif.bash",
		"zsh":        "_fdif",
		"fish":       "fdif.fish",
		"powershell": "fdif.ps1",
	}
	for shell, name := range cases {
		want, err := completionScript(shell)
		if err != nil {
			t.Fatalf("generate %s completion: %v", shell, err)
		}
		path := filepath.Join("..", "..", "completions", name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("%s completion file is stale", shell)
		}
	}
}

func TestParseGitHubRemoteSupportsHTTPSAndSSH(t *testing.T) {
	cases := []string{
		"https://github.com/octo/example.git",
		"https://github.com/octo/example.git/",
		"git@github.com:octo/example.git",
		"ssh://git@github.com/octo/example.git",
	}
	for _, raw := range cases {
		owner, repo, normalized, err := parseGitHubRemote(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if owner != "octo" || repo != "example" || normalized != "https://github.com/octo/example.git" {
			t.Fatalf("parse %q = %s/%s %s", raw, owner, repo, normalized)
		}
	}
}

func TestParseGitHubRemoteRejectsUnsupportedHostAndCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://gitlab.com/octo/example.git",
		"https://token@github.com/octo/example.git",
		"https://github.com/octo/too/many.git",
	} {
		if _, _, _, err := parseGitHubRemote(raw); err == nil {
			t.Fatalf("unsafe remote was accepted: %s", raw)
		}
	}
}

func TestFinishGitHubPreparesEffectsBeforeVerifyAndReturnsPRURL(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "git@github.com:octo/example.git")
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	app.GitHubCredentialID = "github-main"
	if code := app.Run(context.Background(), []string{"finish", "tx_test", "--github", "--title", "Add safe publication"}); code != 0 {
		t.Fatalf("GitHub finish failed: stdout=%s", out.String())
	}
	var names []string
	for _, call := range engine.calls {
		names = append(names, call[0])
	}
	positions := map[string]int{}
	for i, name := range names {
		if _, exists := positions[name]; !exists {
			positions[name] = i
		}
	}
	for _, name := range []string{"seal", "prepare-github-branch", "prepare-github-pr", "verify", "approve", "commit"} {
		if _, ok := positions[name]; !ok {
			t.Fatalf("missing %s in %v", name, names)
		}
	}
	if !(positions["seal"] < positions["prepare-github-branch"] && positions["prepare-github-branch"] < positions["prepare-github-pr"] && positions["prepare-github-pr"] < positions["verify"] && positions["verify"] < positions["commit"]) {
		t.Fatalf("unsafe effect order: %v", names)
	}
	for _, phrase := range []string{"sent to GitHub", "https://github.com/octo/example/pull/42", "futurediff/tx_test", "Current branch"} {
		if !strings.Contains(out.String(), phrase) {
			t.Fatalf("GitHub output missing %q:\n%s", phrase, out.String())
		}
	}
}

func TestFinishGitHubJSONProducesOneDocument(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/octo/example.git")
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, out, _ := newTestApp(t, engine, repo, workspace)
	app.JSON = true
	app.GitHubCredentialID = "github-main"
	if code := app.Run(context.Background(), []string{"finish", "tx_test", "--github"}); code != 0 {
		t.Fatalf("GitHub JSON finish exit code %d: %s", code, out.String())
	}
	var decoded struct {
		Kind   string              `json:"kind"`
		GitHub GitHubPublishResult `json:"github"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid GitHub JSON: %v\n%s", err, out.String())
	}
	if decoded.Kind != "fdif-publish" || decoded.GitHub.PullRequestURL != "https://github.com/octo/example/pull/42" || !decoded.GitHub.Draft {
		t.Fatalf("unexpected GitHub JSON: %+v", decoded)
	}
}

func TestFinishGitHubRequiresCredentialSelection(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/octo/example.git")
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"finish", "tx_test", "--github"}); code == 0 {
		t.Fatal("GitHub finish succeeded without credential selection")
	}
	if !strings.Contains(errOut.String(), "FUTUREDIFF_GITHUB_CREDENTIAL_ID") {
		t.Fatalf("unclear credential error: %s", errOut.String())
	}
}

func TestFinishGitHubRejectsReadyTransactionWithoutPreparedEffects(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/octo/example.git")
	engine := &fakeEngine{state: "ready", repo: repo, workspace: workspace}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	app.GitHubCredentialID = "github-main"
	if code := app.Run(context.Background(), []string{"finish", "tx_test", "--github"}); code == 0 {
		t.Fatal("GitHub effects were incorrectly added after verification")
	}
	if !strings.Contains(errOut.String(), "prepared while the change is sealed") {
		t.Fatalf("unexpected late-selection error: %s", errOut.String())
	}
}

func TestLocalFinishDoesNotPrepareGitHubEffects(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/octo/example.git")
	engine := &fakeEngine{state: "active", repo: repo, workspace: workspace}
	app, _, _ := newTestApp(t, engine, repo, workspace)
	app.GitHubCredentialID = "github-main"
	if code := app.Run(context.Background(), []string{"finish", "tx_test"}); code != 0 {
		t.Fatalf("local finish failed: %d", code)
	}
	for _, call := range engine.calls {
		if strings.HasPrefix(call[0], "prepare-github-") {
			t.Fatalf("local finish prepared GitHub effect: %v", call)
		}
	}
}

func TestParseFinishOptionsRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "body-link.md")
	if err := os.Symlink(bodyFile, link); err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		{"--remote", "origin"},
		{"--github", "--unknown"},
		{"--github", "--remote", "-unsafe"},
		{"--github", "--body", "one", "--body-file", bodyFile},
		{"--github", "--body-file", link},
		{"tx_one", "tx_two"},
	}
	for _, args := range cases {
		if _, err := parseFinishOptions(args, "github-main"); err == nil {
			t.Fatalf("unsafe finish options were accepted: %v", args)
		}
	}
}

func TestParseFinishOptionsSupportsInlineValues(t *testing.T) {
	options, err := parseFinishOptions([]string{"tx_one", "--github", "--remote=upstream", "--base=main", "--title=Safe change"}, "github-main")
	if err != nil {
		t.Fatal(err)
	}
	if !options.Enabled || options.Remote != "upstream" || options.Base != "main" || options.Title != "Safe change" {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestParseGitHubRemoteRejectsUnsafeForms(t *testing.T) {
	for _, raw := range []string{
		"http://github.com/octo/example.git",
		"ftp://github.com/octo/example.git",
		"https://github.com:444/octo/example.git",
		"ssh://git@github.com:2222/octo/example.git",
		"https://github.com/octo/%65xample.git",
		"ssh://user@github.com/octo/example.git",
		"github.com:octo/example.git",
	} {
		if _, _, _, err := parseGitHubRemote(raw); err == nil {
			t.Fatalf("unsafe remote was accepted: %s", raw)
		}
	}
}

func TestMatchingGitHubEffectsRejectsDuplicatesAndBrokenDependency(t *testing.T) {
	target := githubTarget{Owner: "octo", Repo: "example", RemoteURL: "https://github.com/octo/example.git", Head: "futurediff/tx_test", Base: "main", Title: "Title", Body: "Body", Credential: "github-main"}
	branchInput, _ := json.Marshal(githubBranchInput{Owner: target.Owner, Repo: target.Repo, Branch: target.Head, RemoteURL: target.RemoteURL})
	draftInput, _ := json.Marshal(githubDraftInput{Owner: target.Owner, Repo: target.Repo, Head: target.Head, Base: target.Base, Title: target.Title, Body: target.Body, DependsOnEffectID: "wrong"})
	branch := ExternalEffect{EffectID: "branch", AdapterIdentity: githubBranchAdapterID, CredentialID: target.Credential, InputJSON: string(branchInput)}
	draft := ExternalEffect{EffectID: "draft", AdapterIdentity: githubDraftAdapterID, CredentialID: target.Credential, InputJSON: string(draftInput), DependsOn: []string{"wrong"}}
	if _, _, err := matchingGitHubEffects([]ExternalEffect{branch, draft}, target); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("broken dependency was accepted: %v", err)
	}
	if _, _, err := matchingGitHubEffects([]ExternalEffect{branch, branch}, target); err == nil || !strings.Contains(err.Error(), "multiple GitHub branch") {
		t.Fatalf("duplicate branch effects were accepted: %v", err)
	}
}

func TestMatchingGitHubEffectsRejectsChangedBody(t *testing.T) {
	target := githubTarget{Owner: "octo", Repo: "example", RemoteURL: "https://github.com/octo/example.git", Head: "futurediff/tx_test", Base: "main", Title: "Title", Body: "Expected", Credential: "github-main"}
	branchInput, _ := json.Marshal(githubBranchInput{Owner: target.Owner, Repo: target.Repo, Branch: target.Head, RemoteURL: target.RemoteURL})
	draftInput, _ := json.Marshal(githubDraftInput{Owner: target.Owner, Repo: target.Repo, Head: target.Head, Base: target.Base, Title: target.Title, Body: "Different", DependsOnEffectID: "branch"})
	effects := []ExternalEffect{
		{EffectID: "branch", AdapterIdentity: githubBranchAdapterID, CredentialID: target.Credential, InputJSON: string(branchInput)},
		{EffectID: "draft", AdapterIdentity: githubDraftAdapterID, CredentialID: target.Credential, InputJSON: string(draftInput), DependsOn: []string{"branch"}},
	}
	if _, _, err := matchingGitHubEffects(effects, target); err == nil || !strings.Contains(err.Error(), "different GitHub pull-request") {
		t.Fatalf("changed body was accepted: %v", err)
	}
}

func TestGitHubResultRejectsMalformedReceiptURL(t *testing.T) {
	target := githubTarget{Owner: "octo", Repo: "example", Head: "futurediff/tx_test", Base: "main"}
	response := Response{
		Effects:  []ExternalEffect{{EffectID: "draft", AdapterIdentity: githubDraftAdapterID}},
		Receipts: []EffectReceipt{{EffectID: "draft", ProviderResourceID: "github://octo/example/pulls/not-a-number"}},
	}
	result := githubResult(response, target)
	if !result.URLIsFallback || result.PullRequestURL != "https://github.com/octo/example/pulls" {
		t.Fatalf("malformed receipt was trusted: %+v", result)
	}
}

func TestLocalFinishRejectsPreparedExternalActions(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", approved: true, repo: repo, workspace: workspace, effects: []ExternalEffect{{EffectID: "effect_slack", AdapterIdentity: "builtin.slack.message-outbox"}}}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"finish", "tx_test"}); code == 0 {
		t.Fatal("local finish silently committed a prepared external action")
	}
	if !strings.Contains(errOut.String(), "cannot silently commit external actions") {
		t.Fatalf("unclear prepared-effect error: %s", errOut.String())
	}
	for _, call := range engine.calls {
		if call[0] == "commit" {
			t.Fatalf("commit ran despite prepared external action: %v", engine.calls)
		}
	}
}

func TestGuidedPublishRejectsPreparedExternalActions(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := &fakeEngine{state: "ready", approved: true, repo: repo, workspace: workspace, effects: []ExternalEffect{{EffectID: "effect_pr", AdapterIdentity: githubDraftAdapterID}}}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	if code := app.Run(context.Background(), []string{"publish", "tx_test"}); code == 0 {
		t.Fatal("guided publish silently committed an external action")
	}
	if !strings.Contains(errOut.String(), "prepared external actions") {
		t.Fatalf("unclear guided publish error: %s", errOut.String())
	}
}

func TestFinishGitHubRejectsUnrelatedPreparedEffect(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	runGitTest(t, repo, "remote", "add", "origin", "https://github.com/octo/example.git")
	engine := &fakeEngine{state: "sealed", repo: repo, workspace: workspace, effects: []ExternalEffect{{EffectID: "effect_slack", AdapterIdentity: "builtin.slack.message-outbox"}}}
	app, _, errOut := newTestApp(t, engine, repo, workspace)
	app.GitHubCredentialID = "github-main"
	if code := app.Run(context.Background(), []string{"finish", "tx_test", "--github"}); code == 0 {
		t.Fatal("guided GitHub finish accepted an unrelated prepared effect")
	}
	if !strings.Contains(errOut.String(), "cannot commit prepared effect") {
		t.Fatalf("unclear unrelated-effect error: %s", errOut.String())
	}
}
