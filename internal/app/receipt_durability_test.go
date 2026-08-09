package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const receiptSecretToken = "super-secret-token"

// serviceWithReceiptFault prepares a ready, approved transaction and arms a
// test-only receipt-lifecycle fault injector on the ledger just before the
// commit attempt, so the fault targets the first receipt boundary it reaches.
func serviceWithReceiptFault(t *testing.T, fake *appFakeGitHub, inject durablewrite.Injector) (*Service, *ledger.Repository, TransactionView, domain.ExternalEffect, string) {
	t.Helper()
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, effect, digest := prepareReadyTransaction(t, svc, repoPath)
	store.Injector = inject
	return svc, store, ready, effect, digest
}

func assertNoToken(t *testing.T, label string, b []byte) {
	t.Helper()
	if strings.Contains(string(b), receiptSecretToken) {
		t.Fatalf("%s leaked the credential token", label)
	}
}

func TestReceiptFaultBeforeEffectPreventsProviderCall(t *testing.T) {
	for _, op := range []string{durablewrite.OpCreate, durablewrite.OpWrite} {
		t.Run(op, func(t *testing.T) {
			fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
			svc, store, ready, effect, digest := serviceWithReceiptFault(t, fake, durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO}))
			_, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err == nil {
				t.Fatal("expected pre-effect receipt fault")
			}
			if fake.postCalls != 0 {
				t.Fatalf("external effect started despite %s fault: posts=%d", op, fake.postCalls)
			}
			stored, _ := store.ExternalEffect(effect.EffectID)
			if stored.Status != domain.EffectVerified {
				t.Fatalf("effect status=%s", stored.Status)
			}
			tx, _ := store.Get(ready.Transaction.ID)
			if tx.Status != domain.StateNeedsReconciliation {
				t.Fatalf("tx status=%s", tx.Status)
			}
			// Recovery proves the effect absent and returns the transaction to
			// ready; capacity is restored and retry is idempotent.
			recovered, err := svc.Recover(ready.Transaction.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Transaction.Status != domain.StateReady {
				t.Fatalf("post-recovery status=%s", recovered.Transaction.Status)
			}
			committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err != nil {
				t.Fatalf("retry commit failed: %v", err)
			}
			if fake.postCalls != 1 || len(committed.Receipts) != 1 {
				t.Fatalf("retry duplicated or missed: posts=%d receipts=%d", fake.postCalls, len(committed.Receipts))
			}
			b, _ := json.Marshal(committed.Receipts)
			assertNoToken(t, "receipt", b)
		})
	}
}

func TestPostEffectReceiptFaultRequiresReconciliation(t *testing.T) {
	for _, op := range []string{durablewrite.OpShortWrite, durablewrite.OpFileSync, durablewrite.OpRename, durablewrite.OpDirectorySync} {
		t.Run(op, func(t *testing.T) {
			fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
			svc, store, ready, effect, digest := serviceWithReceiptFault(t, fake, durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO}))
			_, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err == nil {
				t.Fatal("expected post-effect receipt fault")
			}
			assertNoToken(t, "commit error", []byte(err.Error()))
			if fake.postCalls != 1 {
				t.Fatalf("provider effect count=%d", fake.postCalls)
			}
			tx, _ := store.Get(ready.Transaction.ID)
			if tx.Status != domain.StateNeedsReconciliation {
				t.Fatalf("tx status=%s", tx.Status)
			}
			stored, _ := store.ExternalEffect(effect.EffectID)
			if stored.Status != domain.EffectCommitting {
				t.Fatalf("effect status=%s", stored.Status)
			}
			// Reconciliation queries the provider, records the completed
			// outcome, and never re-executes the external effect.
			recovered, err := svc.Recover(ready.Transaction.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Transaction.Status != domain.StateCommitted || fake.postCalls != 1 || len(recovered.Receipts) != 1 {
				t.Fatalf("recovery status=%s posts=%d receipts=%d", recovered.Transaction.Status, fake.postCalls, len(recovered.Receipts))
			}
			b, _ := json.Marshal(recovered.Receipts)
			assertNoToken(t, "receipt", b)
		})
	}
}

func TestReceiptFaultClassificationThroughCommit(t *testing.T) {
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
			svc, _, ready, _, digest := serviceWithReceiptFault(t, fake, durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: c.fail}))
			_, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
			if err == nil {
				t.Fatal("expected receipt fault")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestAlreadyPresentOutcomeRecognizedWithoutDuplication(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, _, ready, effect, digest := serviceWithReceiptFault(t, fake, nil)
	// The provider already shows the completed effect; the commit must
	// recognize it and record the receipt without dispatching a new one.
	var p githubdraft.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.pulls = append(fake.pulls, map[string]any{
		"number": 500, "node_id": "node-500", "html_url": "https://github.com/acme/app/pull/500",
		"title": p.Input.Title, "body": p.EffectMarker,
		"head": map[string]any{"ref": p.Input.Head, "sha": fake.refs[p.Input.Head]},
		"base": map[string]any{"ref": p.Input.Base, "sha": fake.refs[p.Input.Base]},
	})
	fake.mu.Unlock()
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if fake.postCalls != 0 {
		t.Fatalf("duplicate external effect dispatched: posts=%d", fake.postCalls)
	}
	if committed.Transaction.Status != domain.StateCommitted || len(committed.Effects) != 1 || committed.Effects[0].Status != domain.EffectCommitted || len(committed.Receipts) != 1 {
		t.Fatalf("commit state: %s effects=%#v receipts=%d", committed.Transaction.Status, committed.Effects, len(committed.Receipts))
	}
	if committed.Receipts[0].ProviderOperationID != "github.pull_request.500" {
		t.Fatalf("receipt=%#v", committed.Receipts[0])
	}
}

func TestAmbiguousOutcomeRefusesBlindRetry(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken, postMode: "ambiguous"}
	svc, store, ready, effect, digest := serviceWithReceiptFault(t, fake, nil)
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected ambiguous outcome")
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("effect status=%s", stored.Status)
	}
	// Recovery must not blindly re-dispatch: it status-queries, finds the
	// committed result, and records the receipt.
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fake.postCalls != 1 || recovered.Transaction.Status != domain.StateCommitted || len(recovered.Receipts) != 1 {
		t.Fatalf("blind retry: posts=%d status=%s receipts=%d", fake.postCalls, recovered.Transaction.Status, len(recovered.Receipts))
	}
}

func TestRepeatedRecoveryDoesNotDuplicateEffects(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: receiptSecretToken}
	svc, store, ready, _, digest := serviceWithReceiptFault(t, fake, durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO}))
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected post-effect receipt fault")
	}
	if _, err := svc.Recover(ready.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	// A second recovery attempt on the now-committed transaction must fail
	// closed without any new provider or receipt side effects.
	if _, err := svc.Recover(ready.Transaction.ID); err == nil {
		t.Fatal("expected second recovery refusal")
	}
	if fake.postCalls != 1 {
		t.Fatalf("posts=%d", fake.postCalls)
	}
	rows, err := store.Events(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(rows)
	assertNoToken(t, "events", encoded)
}
