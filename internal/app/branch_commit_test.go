package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

const branchSecretToken = "branch-super-secret-token"

// fakeBranchRunner simulates the GitHub smart-HTTP git boundary used by the
// create-only branch-publish adapter. The remote branch is absent until the
// first successful create-only push makes it resolve to the pushed commit.
// Tests can force ambiguity (push or status transport errors), authentication
// failure, and foreign-commit conflicts.
type fakeBranchRunner struct {
	mu        sync.Mutex
	oid       string // remote refs/heads/branch OID; empty means absent
	pushCount int
	// modes
	pushError          error // PushCreateOnly returns this error (ambiguous transport)
	statusErr          error // LSRemote returns this error (ambiguous status)
	failStatusFromCall int   // when > 0, fail LSRemote once statusCalls reaches this
	statusCalls        int
	badToken           string // when non-empty, calls with this token fail authentication
}

func (f *fakeBranchRunner) LSRemote(_ context.Context, _, _, _ string, token []byte) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls++
	if f.badToken != "" && string(token) == f.badToken {
		return "", false, errors.New("authentication failed")
	}
	if f.statusErr != nil && (f.failStatusFromCall == 0 || f.statusCalls >= f.failStatusFromCall) {
		return "", false, f.statusErr
	}
	if f.oid == "" {
		return "", false, nil
	}
	return f.oid, true, nil
}

func (f *fakeBranchRunner) PushCreateOnly(_ context.Context, _, _, _, commitOID string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pushError != nil {
		return f.pushError
	}
	f.pushCount++
	f.oid = commitOID
	return nil
}

func newBranchService(t *testing.T, runner *fakeBranchRunner) (*Service, *ledger.Repository, string) {
	t.Helper()
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	_ = os.Mkdir(repoPath, 0o700)
	runGit(t, repoPath, "init", "-b", "main")
	_ = os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600)
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Setenv("FD_TEST_BRANCH_TOKEN", branchSecretToken)
	config := credentials.Config{
		Version: "0.1",
		Adapters: []credentials.AdapterIdentity{{
			ID: githubbranch.AdapterID, Version: githubbranch.AdapterVersion, TrustLevel: credentials.TrustBuiltIn,
			ExecutableDigest: "builtin:" + githubbranch.AdapterID + "@" + githubbranch.AdapterVersion, Enabled: true,
		}},
		Credentials: []credentials.Binding{{
			ID: "branch-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_TEST_BRANCH_TOKEN"},
			AllowedAdapters:     []string{githubbranch.AdapterID},
			AllowedOperations:   []string{githubbranch.ReadOperation, githubbranch.CommitOperation},
			AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "github.com", PathPrefix: "/acme/app.git"}},
			Enabled:             true,
		}},
	}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, store, store)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{},
		Credentials: broker, GitHubBranch: &githubbranch.Adapter{Runner: runner}, CoordinatorID: "test-coordinator",
	}
	return svc, store, repoPath
}

func prepareBranchReadyTransaction(t *testing.T, svc *Service, repoPath string) (TransactionView, domain.ExternalEffect, string) {
	t.Helper()
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	effect, err := svc.PrepareGitHubBranch(context.Background(), created.Transaction.ID, PrepareGitHubBranchRequest{
		CredentialID: "branch-main", Owner: "acme", Repo: "app", Branch: "futurediff/" + created.Transaction.ID,
		RemoteURL: "https://github.com/acme/app.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "basic", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	ready, err := svc.Verify(created.Transaction.ID, contract)
	if err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(created.Transaction.ID, digest, "test-user"); err != nil {
		t.Fatal(err)
	}
	return ready, effect, digest
}

func branchApprovedCommit(t *testing.T, effect domain.ExternalEffect) string {
	t.Helper()
	var prepared githubbranch.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &prepared); err != nil {
		t.Fatal(err)
	}
	return prepared.Input.CommitOID
}

func TestBranchPublishCommitsExactlyOnce(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, store, repoPath := newBranchService(t, runner)
	ready, effect, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	mainBefore := runGit(t, repoPath, "rev-parse", "refs/heads/main")
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status=%s", committed.Transaction.Status)
	}
	if runner.pushCount != 1 {
		t.Fatalf("expected exactly one create-only push, got %d", runner.pushCount)
	}
	if len(committed.Effects) != 1 || committed.Effects[0].Status != domain.EffectCommitted || len(committed.Receipts) != 1 {
		t.Fatalf("effects/receipts: %#v %#v", committed.Effects, committed.Receipts)
	}
	if committed.Receipts[0].ProviderOperationID != "github.git.push" || !strings.HasPrefix(committed.Receipts[0].ProviderResourceID, "github://acme/app/refs/heads/futurediff/") {
		t.Fatalf("receipt=%#v", committed.Receipts[0])
	}
	if got := runGit(t, repoPath, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("source branch mutated: %s -> %s", mainBefore, got)
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectCommitted {
		t.Fatalf("stored effect=%s", stored.Status)
	}
	rows, _ := store.Events(ready.Transaction.ID)
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), branchSecretToken) {
		t.Fatal("secret leaked into transaction events")
	}
}

func TestBranchAlreadyPresentRecognizedWithoutPush(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, _, repoPath := newBranchService(t, runner)
	ready, effect, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	// The approved commit already exists on the remote branch (for example, a
	// previous interrupted push completed): commit must record the receipt
	// without dispatching a new push.
	runner.mu.Lock()
	runner.oid = branchApprovedCommit(t, effect)
	runner.mu.Unlock()
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if runner.pushCount != 0 {
		t.Fatalf("duplicate push dispatched: %d", runner.pushCount)
	}
	if committed.Transaction.Status != domain.StateCommitted || committed.Effects[0].Status != domain.EffectCommitted || len(committed.Receipts) != 1 {
		t.Fatalf("commit state: %s effects=%#v receipts=%d", committed.Transaction.Status, committed.Effects, len(committed.Receipts))
	}
}

func TestBranchForeignCommitAtPreflightIsStale(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, store, repoPath := newBranchService(t, runner)
	ready, _, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	// The remote branch appears at a foreign commit between approval and
	// release: the provider preflight classifies it stale and refuses.
	runner.mu.Lock()
	runner.oid = strings.Repeat("b", 40)
	runner.mu.Unlock()
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected stale branch rejection")
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateStale || runner.pushCount != 0 {
		t.Fatalf("stale handling: status=%s pushes=%d", tx.Status, runner.pushCount)
	}
}

func TestBranchConflictDuringRecoveryIsManualIntervention(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, store, repoPath := newBranchService(t, runner)
	ready, _, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	// The push transport drops the response after the server may or may not
	// have accepted: the outcome is unknown and must not be retried blindly.
	runner.mu.Lock()
	runner.pushError = errors.New("connection reset after upload")
	runner.mu.Unlock()
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected ambiguous push outcome")
	}
	stored, _ := store.ExternalEffect(ready.Effects[0].EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("effect status=%s", stored.Status)
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
	// A concurrent actor took the branch name to a foreign commit: recovery
	// must refuse to rearm or push and require manual intervention.
	runner.mu.Lock()
	runner.pushError = nil
	runner.oid = strings.Repeat("c", 40)
	runner.mu.Unlock()
	if _, err := svc.Recover(ready.Transaction.ID); err == nil {
		t.Fatal("expected manual intervention")
	}
	tx, _ = store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateManualIntervention || runner.pushCount != 0 {
		t.Fatalf("post-recovery: status=%s pushes=%d", tx.Status, runner.pushCount)
	}
}

func TestBranchAmbiguousPushRearmsOnlyWhenProvedAbsent(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, store, repoPath := newBranchService(t, runner)
	ready, effect, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	// The push transport drops the response after the server may or may not
	// have accepted: the outcome is unknown and must not be retried blindly.
	runner.mu.Lock()
	runner.pushError = errors.New("connection reset after upload")
	runner.mu.Unlock()
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected ambiguous push outcome")
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("effect status=%s", stored.Status)
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
	// First recovery: the branch is still absent, so the effect is re-armed
	// and the transaction returns to ready for a safe canonical retry.
	runner.mu.Lock()
	runner.pushError = nil
	runner.mu.Unlock()
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateReady {
		t.Fatalf("recovery status=%s", recovered.Transaction.Status)
	}
	stored, _ = store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectVerified {
		t.Fatalf("effect status=%s", stored.Status)
	}
	// The canonical retry publishes exactly once.
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted || runner.pushCount != 1 {
		t.Fatalf("retry: status=%s pushes=%d", committed.Transaction.Status, runner.pushCount)
	}
}

func TestBranchReceiptFaultAfterPushReconcilesWithoutRepush(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, store, repoPath := newBranchService(t, runner)
	ready, effect, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	// The push succeeds but the durable receipt cannot be persisted.
	store.Injector = durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected post-push receipt fault")
	}
	if runner.pushCount != 1 {
		t.Fatalf("push count=%d", runner.pushCount)
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectCommitting {
		t.Fatalf("effect status=%s", stored.Status)
	}
	// Recovery status-queries the provider, sees the branch at the approved
	// commit, and records the receipt without a second push.
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateCommitted || runner.pushCount != 1 || len(recovered.Receipts) != 1 {
		t.Fatalf("recovery: status=%s pushes=%d receipts=%d", recovered.Transaction.Status, runner.pushCount, len(recovered.Receipts))
	}
}

func TestBranchStatusUnknownRequiresReconciliation(t *testing.T) {
	// The preflight status query succeeds; the commit-time status query fails.
	runner := &fakeBranchRunner{statusErr: errors.New("network partition during ls-remote"), failStatusFromCall: 3}
	svc, store, repoPath := newBranchService(t, runner)
	ready, effect, digest := prepareBranchReadyTransaction(t, svc, repoPath)
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected status-unknown failure")
	}
	if runner.pushCount != 0 {
		t.Fatalf("push happened despite unknown status: %d", runner.pushCount)
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("effect status=%s", stored.Status)
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
}

func TestBranchAuthenticationFailureIsDenied(t *testing.T) {
	runner := &fakeBranchRunner{badToken: "wrong-token"}
	svc, store, repoPath := newBranchService(t, runner)
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	// The credential source resolves to a token the provider rejects; no
	// effect may be persisted and no push may occur.
	t.Setenv("FD_TEST_BRANCH_TOKEN", "wrong-token")
	if _, err := svc.PrepareGitHubBranch(context.Background(), created.Transaction.ID, PrepareGitHubBranchRequest{
		CredentialID: "branch-main", Owner: "acme", Repo: "app", Branch: "futurediff/" + created.Transaction.ID,
		RemoteURL: "https://github.com/acme/app.git",
	}); err == nil {
		t.Fatal("expected authentication failure")
	}
	effects, err := store.ExternalEffects(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 0 {
		t.Fatalf("effect persisted despite auth denial: %#v", effects)
	}
	if runner.pushCount != 0 {
		t.Fatalf("push happened despite auth denial: %d", runner.pushCount)
	}
}

func TestBranchApproveThenChangedMaterialRequiresNewApproval(t *testing.T) {
	runner := &fakeBranchRunner{}
	svc, _, repoPath := newBranchService(t, runner)
	ready, _, digest1 := prepareBranchReadyTransaction(t, svc, repoPath)
	// The workspace material changes after approval; the earlier approval
	// digest must not authorize the new material (same revision signal as
	// CommitContext uses to mark staleness).
	for _, tr := range []struct {
		from, to          domain.TransactionState
		material, invalid bool
	}{
		{domain.StateReady, domain.StateStale, true, true},
		{domain.StateStale, domain.StateVerifying, false, false},
		{domain.StateVerifying, domain.StateReady, false, false},
	} {
		if _, err := svc.Ledger.Transition(ready.Transaction.ID, tr.from, tr.to, "test", "material revision", tr.material, tr.invalid); err != nil {
			t.Fatal(err)
		}
	}
	material, err := svc.ApprovalMaterial(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest2 := material["transaction_digest"]
	if digest2 == digest1 {
		t.Fatal("changed material produced the same approval digest")
	}
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest1); err == nil {
		t.Fatal("changed material reused an earlier approval")
	}
	if runner.pushCount != 0 {
		t.Fatalf("push dispatched with stale approval: %d", runner.pushCount)
	}
}
