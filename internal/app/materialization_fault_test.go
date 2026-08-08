package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

// localReadyTransaction prepares a ready, approved transaction with a local
// workspace change and no external provider effects.
func localReadyTransaction(t *testing.T, svc *Service, repoPath string) (TransactionView, string) {
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
	return ready, digest
}

func futureRef(txID string) string { return "refs/heads/futurediff/" + txID }

func noVisibleFutureBranches(t *testing.T, repoPath string) {
	t.Helper()
	if refs := runGit(t, repoPath, "for-each-ref", "--format=%(refname)", "refs/heads/futurediff/"); refs != "" {
		t.Fatalf("visible future branches: %q", refs)
	}
}

func futureCommitCount(t *testing.T, repoPath, txID string) int {
	t.Helper()
	out := runGit(t, repoPath, "rev-list", "--count", futureRef(txID), "--not", "refs/heads/main")
	n, err := strconv.Atoi(out)
	if err != nil {
		t.Fatalf("rev-list on %s failed: %q", futureRef(txID), out)
	}
	return n
}

func TestLocalMaterializationSucceedsExactlyOnce(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, _, repoPath := newExternalEffectService(t, fake)
	ready, digest := localReadyTransaction(t, svc, repoPath)
	mainBefore := runGit(t, repoPath, "rev-parse", "refs/heads/main")
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status=%s", committed.Transaction.Status)
	}
	if n := futureCommitCount(t, repoPath, ready.Transaction.ID); n != 1 {
		t.Fatalf("expected exactly one future commit, got %d", n)
	}
	if got := runGit(t, repoPath, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("source branch mutated: %s -> %s", mainBefore, got)
	}
	live, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if string(live) != "current\n" {
		t.Fatalf("live checkout changed: %q", live)
	}
	if got := runGit(t, repoPath, "show", futureRef(ready.Transaction.ID)+":README.md"); got != "future" {
		t.Fatalf("future ref mismatch: %q", got)
	}
}

func TestLocalMaterializationFaultsFailClosed(t *testing.T) {
	boundaries := []string{staging.OpCommitTree, staging.OpApplyIndex, staging.OpWriteTree, staging.OpUpdateRef, staging.OpWorktreeAdd}
	for _, op := range boundaries {
		t.Run(op, func(t *testing.T) {
			fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
			svc, store, repoPath := newExternalEffectService(t, fake)
			ready, digest := localReadyTransaction(t, svc, repoPath)
			mainBefore := runGit(t, repoPath, "rev-parse", "refs/heads/main")
			svc.Staging.Injector = durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO})
			_, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err == nil {
				t.Fatalf("expected %s fault", op)
			}
			// No false success: the transaction enters reconciliation and no
			// visible future branch or source mutation exists.
			tx, _ := store.Get(ready.Transaction.ID)
			if tx.Status != domain.StateNeedsReconciliation {
				t.Fatalf("tx status=%s", tx.Status)
			}
			if got := runGit(t, repoPath, "rev-parse", "refs/heads/main"); got != mainBefore {
				t.Fatalf("source branch mutated: %s -> %s", mainBefore, got)
			}
			noVisibleFutureBranches(t, repoPath)
			// Recovery sees the known-absent local effect and returns the
			// transaction to ready; retry after restored capacity is safe and
			// idempotent (exactly one branch and one commit).
			recovered, err := svc.Recover(ready.Transaction.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Transaction.Status != domain.StateReady {
				t.Fatalf("post-recovery status=%s", recovered.Transaction.Status)
			}
			committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			if committed.Transaction.Status != domain.StateCommitted {
				t.Fatalf("retry status=%s", committed.Transaction.Status)
			}
			if n := futureCommitCount(t, repoPath, ready.Transaction.ID); n != 1 {
				t.Fatalf("expected exactly one future commit after retry, got %d", n)
			}
		})
	}
}

func TestMaterializedRefPersistenceFaultEntersRecovery(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, digest := localReadyTransaction(t, svc, repoPath)
	mainBefore := runGit(t, repoPath, "rev-parse", "refs/heads/main")
	store.Injector = durablewrite.NewOneShot(map[string]error{ledger.OpMaterializedRef: durablewrite.ErrIO})
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected materialized_ref persistence fault")
	}
	// The local Git effect (commit object and safe branch) is known present
	// even though the durable record failed.
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
	if got := runGit(t, repoPath, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("source branch mutated: %s -> %s", mainBefore, got)
	}
	refBefore := runGit(t, repoPath, "rev-parse", futureRef(ready.Transaction.ID)+"^{commit}")
	// Recovery inspects the existing ref instead of blindly repeating Git
	// creation: the ref identity must not change and no duplicate commit may
	// appear.
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateCommitted {
		t.Fatalf("recovery status=%s", recovered.Transaction.Status)
	}
	refAfter := runGit(t, repoPath, "rev-parse", futureRef(ready.Transaction.ID)+"^{commit}")
	if refAfter != refBefore {
		t.Fatalf("recovery re-created the ref: %s -> %s", refBefore, refAfter)
	}
	if n := futureCommitCount(t, repoPath, ready.Transaction.ID); n != 1 {
		t.Fatalf("expected exactly one future commit, got %d", n)
	}
}

func TestKnownPresentRefRecognizedIdempotently(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, digest := localReadyTransaction(t, svc, repoPath)
	ws, err := store.Workspace(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := store.Patch(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a completed materialization whose durable record is missing
	// (for example, a crashed process after update-ref).
	predicted, err := svc.Staging.PredictMaterializedRef(ws, patch)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "update-ref", predicted.RefName, predicted.CommitOID)
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status=%s", committed.Transaction.Status)
	}
	if got := runGit(t, repoPath, "rev-parse", futureRef(ready.Transaction.ID)+"^{commit}"); got != predicted.CommitOID {
		t.Fatalf("existing commit not recognized: got %s want %s", got, predicted.CommitOID)
	}
	if n := futureCommitCount(t, repoPath, ready.Transaction.ID); n != 1 {
		t.Fatalf("duplicate commit created: %d", n)
	}
}

func TestChangedMaterialRequiresNewApproval(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, _, repoPath := newExternalEffectService(t, fake)
	ready, digest1 := localReadyTransaction(t, svc, repoPath)
	// The reviewed material changes after approval: the earlier approval
	// digest must not authorize the new material. The revision signal bumps
	// material_revision and invalidates the stored approval (the same
	// transition CommitContext uses to mark staleness); returning through the
	// verification states leaves the transaction ready with no stored
	// approval, so digest1 is rejected at BeginCommit.
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
	if _, err := svc.Approve(ready.Transaction.ID, digest2, "test-user"); err != nil {
		t.Fatal(err)
	}
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest2)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status=%s", committed.Transaction.Status)
	}
	if got := runGit(t, repoPath, "show", futureRef(ready.Transaction.ID)+":README.md"); got != "future" {
		t.Fatalf("committed material mismatch: %q", got)
	}
}

func TestMaterializationFaultClassificationThroughCommit(t *testing.T) {
	cases := []struct {
		name string
		fail error
		want string
	}{
		{"enospc", durablewrite.ErrDiskFull, "disk_full"},
		{"edquot", durablewrite.ErrQuotaExceeded, "quota_exceeded"},
		{"erofs", durablewrite.ErrReadOnlyFilesystem, "filesystem_read_only"},
		{"eio", durablewrite.ErrIO, "durable_write_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
			svc, _, repoPath := newExternalEffectService(t, fake)
			ready, digest := localReadyTransaction(t, svc, repoPath)
			svc.Staging.Injector = durablewrite.NewOneShot(map[string]error{staging.OpCommitTree: c.fail})
			_, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err == nil {
				t.Fatal("expected fault")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
			if strings.Contains(err.Error(), fake.token) {
				t.Fatal("credential token leaked into commit error")
			}
		})
	}
}
