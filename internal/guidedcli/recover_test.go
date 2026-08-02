package guidedcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recoverEngine is a fake daemon for recover tests. It supports the commands
// the guided recover flow needs: health, list, get, recover.
type recoverEngine struct {
	daemonDown   bool
	transactions map[string]string // tx id -> status
	order        []string
	recoverTo    string // status after a successful recover
	recoverErr   error
	workspace    *Workspace // included in get responses
	calls        [][]string
}

func newRecoverEngine(tx map[string]string) *recoverEngine {
	engine := &recoverEngine{transactions: tx, recoverTo: "ready"}
	for id := range tx {
		engine.order = append(engine.order, id)
	}
	return engine
}

func gitCommonDir(t *testing.T, repo string) string {
	t.Helper()
	out, err := gitOutput(context.Background(), "git", repo, "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	value := strings.TrimSpace(out)
	if !filepath.IsAbs(value) {
		value = filepath.Join(repo, value)
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func (f *recoverEngine) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	if f.daemonDown {
		return nil, &CommandError{Command: "futurediff health", ExitCode: 1, Stderr: "connect: connection refused"}
	}
	switch args[0] {
	case "health":
		return []byte(`{"status":"ok"}`), nil
	case "list":
		transactions := make([]Transaction, 0)
		for _, id := range f.order {
			transactions = append(transactions, Transaction{TransactionID: id, Status: f.transactions[id]})
		}
		data, _ := json.Marshal(Response{Transactions: transactions})
		return data, nil
	case "get":
		id := args[1]
		status, ok := f.transactions[id]
		if !ok {
			return nil, &CommandError{Command: "futurediff get " + id, ExitCode: 1, Stderr: `daemon returned 404 Not Found: {"error":"not_found"}`}
		}
		var ws *Workspace
		if f.workspace != nil {
			wsCopy := *f.workspace
			wsCopy.TransactionID = id
			ws = &wsCopy
		}
		data, _ := json.Marshal(Response{Transaction: &Transaction{TransactionID: id, Status: status}, Workspace: ws})
		return data, nil
	case "recover":
		id := args[1]
		status, ok := f.transactions[id]
		if !ok {
			return nil, &CommandError{Command: "futurediff recover " + id, ExitCode: 1, Stderr: `daemon returned 404 Not Found: {"error":"not_found"}`}
		}
		if status != "committing" && status != "needs_reconciliation" {
			return nil, &CommandError{Command: "futurediff recover " + id, ExitCode: 409, Stderr: fmt.Sprintf(`{"error":"recovery_failed","message":"transaction %s is not recoverable"}`, id)}
		}
		if f.recoverErr != nil {
			return nil, f.recoverErr
		}
		f.transactions[id] = f.recoverTo
		data, _ := json.Marshal(Response{Transaction: &Transaction{TransactionID: id, Status: f.recoverTo}, Workspace: f.workspace})
		return data, nil
	default:
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
}

func newRecoverApp(t *testing.T, engine *recoverEngine, repo, workspace string, yes, jsonMode bool) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	store := StateStore{Path: filepath.Join(realTempDir(t), "current.json")}
	if err := store.Save("tx_selected", repo); err != nil {
		t.Fatal(err)
	}
	renderer := Renderer{Out: out, Err: errOut, Color: false, Unicode: false}
	app := &App{
		In: strings.NewReader(""), Out: out, Err: errOut,
		Engine: engine, Store: store, Renderer: renderer,
		Binary: "futurediff", DaemonBinary: "futurediffd",
		JSON: jsonMode, Yes: yes, Interactive: false, GitBinary: "git",
	}
	app.Daemon = DaemonManager{Engine: engine, Binary: "futurediffd"}
	return app, out, errOut
}

func decodeRecoveryReport(t *testing.T, out *bytes.Buffer) RecoveryReport {
	t.Helper()
	var report RecoveryReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode recovery report %q: %v", out.String(), err)
	}
	if report.Kind != "recovery_report" {
		t.Fatalf("kind = %q, want recovery_report", report.Kind)
	}
	return report
}

func TestRecoverCommittingToCommitted(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committing"})
	engine.recoverTo = "committed"
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonRecovered {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonRecovered)
	}
	if report.TransactionID != "tx_selected" || report.CurrentStatus != "committed" {
		t.Fatalf("report = %+v", report)
	}
	if report.RecoveryRequired {
		t.Fatal("recovery should not be required after success")
	}
	// Selection pointer must be cleared for a terminal outcome.
	if _, err := app.Store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selection not cleared after terminal recovery: %v", err)
	}
	var recovered bool
	for _, call := range engine.calls {
		if call[0] == "recover" && call[1] == "tx_selected" {
			recovered = true
		}
	}
	if !recovered {
		t.Fatalf("canonical recover was not invoked: %v", engine.calls)
	}
}

func TestRecoverNeedsReconciliationToReady(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "needs_reconciliation"})
	engine.recoverTo = "ready"
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover", "--yes"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonRecovered || report.CurrentStatus != "ready" {
		t.Fatalf("report = %+v", report)
	}
	// Non-terminal outcome: selection is refreshed, not cleared.
	if _, err := app.Store.Load(); err != nil {
		t.Fatalf("selection should be retained for ready: %v", err)
	}
}

func TestRecoverRequiresYesAndRefusesInJSON(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committing"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("JSON refusal should exit 0 with a report: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonRecoveryRequired {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonRecoveryRequired)
	}
	if !report.RecoveryRequired || !report.SafeToRetry {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.RecommendedAction, "--yes") {
		t.Fatalf("recommended action should mention --yes: %q", report.RecommendedAction)
	}
	for _, call := range engine.calls {
		if call[0] == "recover" {
			t.Fatalf("recover was invoked without --yes: %v", engine.calls)
		}
	}
}

func TestRecoverTerminalSelectionClearsPointer(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committed"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonTerminalSelection {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonTerminalSelection)
	}
	if !report.SelectionRepaired {
		t.Fatal("terminal selection should report selection_repaired")
	}
	if _, err := app.Store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pointer not cleared for terminal selection: %v", err)
	}
	for _, call := range engine.calls {
		if call[0] == "recover" {
			t.Fatalf("recover invoked for a terminal selection: %v", engine.calls)
		}
	}
}

func TestRecoverTerminalSelectionWithoutYesKeepsPointer(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "aborted"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonTerminalSelection {
		t.Fatalf("reason_code = %q", report.ReasonCode)
	}
	if report.SelectionRepaired {
		t.Fatal("pointer repaired without --yes")
	}
	if _, err := app.Store.Load(); err != nil {
		t.Fatalf("pointer should be retained without --yes: %v", err)
	}
}

func TestRecoverStaleSelection(t *testing.T) {
	engine := newRecoverEngine(map[string]string{})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonSelectionTransactionMissing {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonSelectionTransactionMissing)
	}
	if report.TransactionID != "tx_selected" {
		t.Fatalf("report = %+v", report)
	}
	if report.SelectionRepaired {
		t.Fatal("pointer repaired without --yes")
	}
}

func TestRecoverStaleSelectionWithYesClears(t *testing.T) {
	engine := newRecoverEngine(map[string]string{})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover", "--yes"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonSelectionTransactionMissing {
		t.Fatalf("reason_code = %q", report.ReasonCode)
	}
	if _, err := app.Store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale pointer not cleared with --yes: %v", err)
	}
}

func TestRecoverNoSelectionSingleEligible(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_only": "active"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	// No stored selection: recovery must not silently pick the sole change.
	if err := app.Store.Clear(); err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonNoRecoveryNeeded {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonNoRecoveryNeeded)
	}
	if report.TransactionID != "tx_only" {
		t.Fatalf("report = %+v", report)
	}
	for _, call := range engine.calls {
		if call[0] == "recover" {
			t.Fatalf("recover silently invoked: %v", engine.calls)
		}
	}
	if _, err := app.Store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("selection was silently created: %v", err)
	}
}

func TestRecoverNoSelectionMultipleEligible(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_a": "active", "tx_b": "sealed"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if err := app.Store.Clear(); err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonMultipleTransactions {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonMultipleTransactions)
	}
}

func TestRecoverNoSelectionZeroEligible(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_done": "committed"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if err := app.Store.Clear(); err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonNoTransactions {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonNoTransactions)
	}
}

func TestRecoverInvalidSelectionFile(t *testing.T) {
	engine := newRecoverEngine(map[string]string{})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if err := os.WriteFile(app.Store.Path, []byte(`{invalid json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonInvalidSelectionFile {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonInvalidSelectionFile)
	}
}

func TestRecoverDaemonUnavailable(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "active"})
	engine.daemonDown = true
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonDaemonUnavailable {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonDaemonUnavailable)
	}
	if !report.SafeToRetry || !strings.Contains(report.RecommendedAction, "daemon") {
		t.Fatalf("report = %+v", report)
	}
}

func TestRecoverWorkspaceMissing(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "active"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/missing-workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonWorkspaceMissing {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonWorkspaceMissing)
	}
	if report.WorkspaceAvailable {
		t.Fatal("workspace_available should be false")
	}
	if !strings.Contains(report.RecommendedAction, "abort") {
		t.Fatalf("missing workspace should recommend abort: %q", report.RecommendedAction)
	}
	for _, call := range engine.calls {
		if call[0] == "recover" {
			t.Fatalf("recover invoked with a missing workspace: %v", engine.calls)
		}
	}
}

func TestRecoverWorkspaceIdentityMismatch(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	// The recorded workspace identity points at a different git common dir
	// than the directory at the workspace path actually has.
	other := makeRepoAtPath(t, filepath.Join(realTempDir(t), "other"))
	common := gitCommonDir(t, other)
	engine := newRecoverEngine(map[string]string{"tx_selected": "active"})
	engine.workspace = &Workspace{
		WorkspacePath: workspace,
		GitCommonDir:  common,
	}
	app, out, _ := newRecoverApp(t, engine, repo, workspace, false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonWorkspaceIdentityMismatch {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonWorkspaceIdentityMismatch)
	}
}

func TestRecoverWorkspaceHealthyNeedsNoRecovery(t *testing.T) {
	repo, workspace := makeRepoAndWorkspace(t)
	engine := newRecoverEngine(map[string]string{"tx_selected": "ready"})
	engine.workspace = &Workspace{
		WorkspacePath: workspace,
		GitCommonDir:  gitCommonDir(t, repo),
	}
	app, out, _ := newRecoverApp(t, engine, repo, workspace, false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonNoRecoveryNeeded {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonNoRecoveryNeeded)
	}
	if !report.WorkspaceAvailable {
		t.Fatal("workspace_available should be true")
	}
	if report.CurrentStatus != "ready" {
		t.Fatalf("report = %+v", report)
	}
}

func TestRecoverExplicitID(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_other": "committing"})
	engine.recoverTo = "committed"
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover", "tx_other"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.TransactionID != "tx_other" || report.ReasonCode != reasonRecovered {
		t.Fatalf("report = %+v", report)
	}
}

func TestRecoverExplicitIDMissing(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "active"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover", "tx_ghost"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonSelectionTransactionMissing {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonSelectionTransactionMissing)
	}
	if report.TransactionID != "tx_ghost" {
		t.Fatalf("report = %+v", report)
	}
}

func TestRecoverIdempotentAfterCommitted(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committed"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", true, true)
	if code := app.Run(context.Background(), []string{"recover", "--yes"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonTerminalSelection {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonTerminalSelection)
	}
	for _, call := range engine.calls {
		if call[0] == "recover" {
			t.Fatalf("canonical recover invoked on an already-finished change: %v", engine.calls)
		}
	}
}

func TestRecoverPropagatesCanonicalError(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committing"})
	engine.recoverErr = &CommandError{Command: "futurediff recover tx_selected", ExitCode: 500, Stderr: `{"error":"internal","message":"boom"}`}
	app, out, errOut := newRecoverApp(t, engine, "/repo", "/workspace", true, false)
	if code := app.Run(context.Background(), []string{"recover"}); code != 500 {
		t.Fatalf("exit code = %d, want 500 (canonical error preserved)", code)
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("canonical error not surfaced: %s %s", out.String(), errOut.String())
	}
}

func TestRecoverManualIntervention(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "manual_intervention"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonRecoveryAmbiguous {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonRecoveryAmbiguous)
	}
}

func TestRecoverJSONContractFields(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_selected": "committing"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"kind", "reason_code", "transaction_id", "current_status",
		"recovery_required", "safe_to_retry", "recommended_action",
		"workspace_available", "selection_repaired",
	} {
		if _, ok := raw[field]; !ok {
			t.Fatalf("JSON contract missing field %q: %s", field, out.String())
		}
	}
}

func TestUseClearRemovesSelection(t *testing.T) {
	engine := newRecoverEngine(map[string]string{})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if code := app.Run(context.Background(), []string{"use", "--clear"}); code != 0 {
		t.Fatalf("use --clear failed: %s", out.String())
	}
	if _, err := app.Store.Load(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("use --clear did not remove selection: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("use --clear output is not JSON: %s", out.String())
	}
}

func TestStrictResolveFailsOnStaleSelection(t *testing.T) {
	engine := newRecoverEngine(map[string]string{})
	app, _, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	// tx_selected is stored but the daemon does not know it; strict resolve
	// must fail instead of silently clearing and re-picking.
	_, err := app.resolveTransaction(context.Background(), "", false, true)
	if err == nil {
		t.Fatal("strict resolve accepted a stale selection")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("error should name the unavailable change: %v", err)
	}
	if _, loadErr := app.Store.Load(); loadErr != nil {
		t.Fatalf("strict resolve cleared the pointer: %v", loadErr)
	}
}

func TestLenientResolveFallsBackOnMissingSelection(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_only": "active"})
	app, _, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if err := app.Store.Clear(); err != nil {
		t.Fatal(err)
	}
	id, err := app.resolveTransaction(context.Background(), "", false, false)
	if err != nil {
		t.Fatalf("lenient resolve failed: %v", err)
	}
	if id != "tx_only" {
		t.Fatalf("id = %s, want tx_only", id)
	}
}
func TestLenientResolveFallsBackWithoutRepairingStalePointer(t *testing.T) {
	engine := newRecoverEngine(map[string]string{"tx_only": "active"})
	app, _, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	// tx_selected is stored but the daemon does not know it. A lenient
	// (read-only) resolve may fall back to the sole eligible change, but it
	// must not silently repair the pointer (ADR-098 D2).
	id, err := app.resolveTransaction(context.Background(), "", false, false)
	if err != nil {
		t.Fatalf("lenient resolve failed: %v", err)
	}
	if id != "tx_only" {
		t.Fatalf("id = %s, want tx_only", id)
	}
	current, loadErr := app.Store.Load()
	if loadErr != nil {
		t.Fatalf("lenient resolve dropped the pointer: %v", loadErr)
	}
	if current.TransactionID != "tx_selected" {
		t.Fatalf("lenient resolve repaired the pointer: got %s, want tx_selected", current.TransactionID)
	}
}

func TestRecoverUnreadableSelectionFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	engine := newRecoverEngine(map[string]string{"tx_selected": "active"})
	app, out, _ := newRecoverApp(t, engine, "/repo", "/workspace", false, true)
	if err := os.Chmod(app.Store.Path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(app.Store.Path, 0o600) })
	if code := app.Run(context.Background(), []string{"recover"}); code != 0 {
		t.Fatalf("recover failed: %s", out.String())
	}
	report := decodeRecoveryReport(t, out)
	if report.ReasonCode != reasonInvalidSelectionFile {
		t.Fatalf("reason_code = %q, want %q", report.ReasonCode, reasonInvalidSelectionFile)
	}
}
