package postgresstate

import (
	"context"
	"testing"
	"time"

	"github.com/futurediff/futurediff/control-plane/coordinator"
	"github.com/futurediff/futurediff/control-plane/domain"
	"github.com/futurediff/futurediff/internal/testpostgres"
)

func TestCoordinatorEnginePersistsTransitionsInPostgres(t *testing.T) {
	ctx := context.Background()
	instance := testpostgres.Start(t)
	bundle := Open(instance.DB)
	if err := bundle.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap postgres store: %v", err)
	}
	if err := bundle.SeedTransaction(ctx, "tx_pg", domain.TransactionStateAwaitingApproval); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	for _, effectID := range []string{"eff_repo", "eff_github"} {
		if err := bundle.SeedEffect(ctx, effectID, domain.EffectStateVerified); err != nil {
			t.Fatalf("seed effect %s: %v", effectID, err)
		}
	}
	engine := coordinator.Engine{
		Transactions: bundle.Transactions,
		Effects:      bundle.Effects,
		Ledger:       bundle.Ledger,
		Approvals:    bundle.Approvals,
		Policy:       allowPolicy{},
		Now:          func() time.Time { return time.Date(2026, 7, 26, 15, 0, 0, 0, time.UTC) },
	}

	ref := domain.ApprovalSnapshotRef{SnapshotID: "snap_pg", Version: "0.1", Hash: "hash_pg"}
	if err := engine.Approve(ctx, "tx_pg", ref, []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := engine.StartCommit(ctx, "tx_pg", []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("start commit: %v", err)
	}
	if err := engine.EnterReconciling(ctx, "tx_pg", "ambiguity_or_lease_loss", []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("enter reconciling: %v", err)
	}
	if err := engine.MarkCommitted(ctx, "tx_pg", []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("mark committed: %v", err)
	}

	transactionState, err := bundle.Transactions.Get(ctx, "tx_pg")
	if err != nil {
		t.Fatalf("load transaction state: %v", err)
	}
	if transactionState != domain.TransactionStateCommitted {
		t.Fatalf("expected committed transaction state, got %s", transactionState)
	}
	for _, effectID := range []string{"eff_repo", "eff_github"} {
		effectState, err := bundle.Effects.Get(ctx, effectID)
		if err != nil {
			t.Fatalf("load effect state %s: %v", effectID, err)
		}
		if effectState != domain.EffectStateCommitted {
			t.Fatalf("expected committed effect state for %s, got %s", effectID, effectState)
		}
	}
	approvalRef, err := bundle.Approvals.Get(ctx, "tx_pg")
	if err != nil {
		t.Fatalf("load approval state: %v", err)
	}
	if approvalRef.Hash != ref.Hash {
		t.Fatalf("expected persisted approval hash %s, got %#v", ref.Hash, approvalRef)
	}
	ledger, err := bundle.Ledger.ListByTransaction(ctx, "tx_pg")
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	if len(ledger) != 4 {
		t.Fatalf("expected four coordinator ledger rows, got %d", len(ledger))
	}
	if ledger[0].NextState != string(domain.TransactionStateReadyToCommit) || ledger[3].Reason != "reconciled_committed" {
		t.Fatalf("unexpected coordinator ledger rows: %#v", ledger)
	}
}

type allowPolicy struct{}

func (allowPolicy) EvaluateCommit(ctx context.Context, transactionID string) error {
	_ = ctx
	_ = transactionID
	return nil
}
