package coordinator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/futurediff/futurediff/control-plane/domain"
)

func TestApproveTransitionsToReadyToCommit(t *testing.T) {
	txStore := &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_approval": domain.TransactionStateAwaitingApproval}}
	effectStore := &memoryEffectStore{states: map[string]domain.EffectState{"eff_repo": domain.EffectStateVerified, "eff_github": domain.EffectStateVerified}}
	ledger := &memoryLedger{}
	approvals := &memoryApprovalStore{}
	policy := &allowPolicy{}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	engine := Engine{Transactions: txStore, Effects: effectStore, Ledger: ledger, Approvals: approvals, Policy: policy, Now: func() time.Time { return now }}

	ref := domain.ApprovalSnapshotRef{SnapshotID: "snap_123", Version: "0.1", Hash: "hash_approval"}
	if err := engine.Approve(context.Background(), "tx_approval", ref, []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("approve transition: %v", err)
	}
	if got := txStore.states["tx_approval"]; got != domain.TransactionStateReadyToCommit {
		t.Fatalf("expected ready-to-commit state, got %s", got)
	}
	for _, effectID := range []string{"eff_repo", "eff_github"} {
		if got := effectStore.states[effectID]; got != domain.EffectStateApproved {
			t.Fatalf("expected approved effect state for %s, got %s", effectID, got)
		}
	}
	if approvals.refs["tx_approval"].Hash != ref.Hash {
		t.Fatalf("expected approval ref to persist, got %#v", approvals.refs["tx_approval"])
	}
	if len(ledger.records) != 1 {
		t.Fatalf("expected one ledger record, got %d", len(ledger.records))
	}
	if ledger.records[0].NextState != string(domain.TransactionStateReadyToCommit) {
		t.Fatalf("unexpected ledger transition: %#v", ledger.records[0])
	}
}

func TestInvalidateApprovalTransitionsBackToActive(t *testing.T) {
	txStore := &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_ready": domain.TransactionStateReadyToCommit}}
	effectStore := &memoryEffectStore{states: map[string]domain.EffectState{"eff_repo": domain.EffectStateApproved}}
	ledger := &memoryLedger{}
	approvals := &memoryApprovalStore{refs: map[string]domain.ApprovalSnapshotRef{"tx_ready": {SnapshotID: "snap_ready", Version: "0.1", Hash: "hash_ready"}}}
	engine := Engine{Transactions: txStore, Effects: effectStore, Ledger: ledger, Approvals: approvals, Policy: &allowPolicy{}, Now: func() time.Time { return time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC) }}

	if err := engine.InvalidateApproval(context.Background(), "tx_ready", "prepared fingerprint changed", []string{"eff_repo"}); err != nil {
		t.Fatalf("invalidate approval: %v", err)
	}
	if got := txStore.states["tx_ready"]; got != domain.TransactionStateActive {
		t.Fatalf("expected active state after invalidation, got %s", got)
	}
	if got := effectStore.states["eff_repo"]; got != domain.EffectStatePrepared {
		t.Fatalf("expected prepared effect state after invalidation, got %s", got)
	}
	if approvals.invalidations["tx_ready"] != "prepared fingerprint changed" {
		t.Fatalf("unexpected invalidation reason: %#v", approvals.invalidations)
	}
	if len(ledger.records) != 1 || ledger.records[0].NextState != string(domain.TransactionStateActive) {
		t.Fatalf("unexpected ledger records: %#v", ledger.records)
	}
}

func TestApproveRejectsWrongState(t *testing.T) {
	engine := Engine{
		Transactions: &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_bad": domain.TransactionStateActive}},
		Effects:      &memoryEffectStore{states: map[string]domain.EffectState{}},
		Approvals:    &memoryApprovalStore{},
		Policy:       &allowPolicy{},
	}
	ref := domain.ApprovalSnapshotRef{SnapshotID: "snap_bad", Version: "0.1", Hash: "hash_bad"}
	if err := engine.Approve(context.Background(), "tx_bad", ref, nil); err == nil {
		t.Fatal("expected wrong-state approval to fail")
	}
}

func TestStartCommitTransitionsEffectsAndTransaction(t *testing.T) {
	txStore := &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_commit": domain.TransactionStateReadyToCommit}}
	effectStore := &memoryEffectStore{states: map[string]domain.EffectState{"eff_repo": domain.EffectStateApproved, "eff_github": domain.EffectStateApproved}}
	ledger := &memoryLedger{}
	engine := Engine{Transactions: txStore, Effects: effectStore, Ledger: ledger, Approvals: &memoryApprovalStore{}, Policy: &allowPolicy{}, Now: func() time.Time { return time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC) }}

	if err := engine.StartCommit(context.Background(), "tx_commit", []string{"eff_repo", "eff_github"}); err != nil {
		t.Fatalf("start commit: %v", err)
	}
	if got := txStore.states["tx_commit"]; got != domain.TransactionStateCommitting {
		t.Fatalf("expected committing state, got %s", got)
	}
	for _, effectID := range []string{"eff_repo", "eff_github"} {
		if got := effectStore.states[effectID]; got != domain.EffectStateCommitting {
			t.Fatalf("expected committing effect state for %s, got %s", effectID, got)
		}
	}
	if len(ledger.records) != 1 || ledger.records[0].NextState != string(domain.TransactionStateCommitting) {
		t.Fatalf("unexpected ledger records: %#v", ledger.records)
	}
}

func TestReconcilingAndCommittedTransitions(t *testing.T) {
	txStore := &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_reconcile": domain.TransactionStateCommitting}}
	effectStore := &memoryEffectStore{states: map[string]domain.EffectState{"eff_github": domain.EffectStateCommitting}}
	ledger := &memoryLedger{}
	engine := Engine{Transactions: txStore, Effects: effectStore, Ledger: ledger, Approvals: &memoryApprovalStore{}, Policy: &allowPolicy{}, Now: func() time.Time { return time.Date(2026, 7, 26, 13, 15, 0, 0, time.UTC) }}

	if err := engine.EnterReconciling(context.Background(), "tx_reconcile", "ambiguity_or_lease_loss", []string{"eff_github"}); err != nil {
		t.Fatalf("enter reconciling: %v", err)
	}
	if got := txStore.states["tx_reconcile"]; got != domain.TransactionStateReconciling {
		t.Fatalf("expected reconciling state, got %s", got)
	}
	if got := effectStore.states["eff_github"]; got != domain.EffectStateUnknown {
		t.Fatalf("expected unknown effect state, got %s", got)
	}
	if err := engine.MarkCommitted(context.Background(), "tx_reconcile", []string{"eff_github"}); err != nil {
		t.Fatalf("mark committed from reconcile: %v", err)
	}
	if got := txStore.states["tx_reconcile"]; got != domain.TransactionStateCommitted {
		t.Fatalf("expected committed state, got %s", got)
	}
	if got := effectStore.states["eff_github"]; got != domain.EffectStateCommitted {
		t.Fatalf("expected committed effect state, got %s", got)
	}
	if len(ledger.records) != 2 || ledger.records[1].Reason != "reconciled_committed" {
		t.Fatalf("unexpected ledger records: %#v", ledger.records)
	}
}

func TestCompensationAndManualInterventionTransitions(t *testing.T) {
	txStore := &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_comp": domain.TransactionStateCommitting, "tx_manual": domain.TransactionStateCompensating}}
	effectStore := &memoryEffectStore{states: map[string]domain.EffectState{"eff_github": domain.EffectStateCommitted}}
	ledger := &memoryLedger{}
	engine := Engine{Transactions: txStore, Effects: effectStore, Ledger: ledger, Approvals: &memoryApprovalStore{}, Policy: &allowPolicy{}, Now: func() time.Time { return time.Date(2026, 7, 26, 13, 45, 0, 0, time.UTC) }}

	if err := engine.BeginCompensation(context.Background(), "tx_comp", "partial_commit_needs_compensation", []string{"eff_github"}); err != nil {
		t.Fatalf("begin compensation: %v", err)
	}
	if got := txStore.states["tx_comp"]; got != domain.TransactionStateCompensating {
		t.Fatalf("expected compensating state, got %s", got)
	}
	if got := effectStore.states["eff_github"]; got != domain.EffectStateCompensating {
		t.Fatalf("expected compensating effect state, got %s", got)
	}
	if err := engine.MarkCompensated(context.Background(), "tx_comp", []string{"eff_github"}); err != nil {
		t.Fatalf("mark compensated: %v", err)
	}
	if got := txStore.states["tx_comp"]; got != domain.TransactionStateCompensated {
		t.Fatalf("expected compensated state, got %s", got)
	}
	if got := effectStore.states["eff_github"]; got != domain.EffectStateCompensated {
		t.Fatalf("expected compensated effect state, got %s", got)
	}
	if err := engine.RequireManualIntervention(context.Background(), "tx_manual", "compensation_exhausted"); err != nil {
		t.Fatalf("require manual intervention: %v", err)
	}
	if got := txStore.states["tx_manual"]; got != domain.TransactionStateFailedManualIntervention {
		t.Fatalf("expected failed manual intervention state, got %s", got)
	}
	if len(ledger.records) != 3 || ledger.records[2].NextState != string(domain.TransactionStateFailedManualIntervention) {
		t.Fatalf("unexpected ledger records: %#v", ledger.records)
	}
}

func TestStartCommitRejectsWrongState(t *testing.T) {
	engine := Engine{Transactions: &memoryTransactionStore{states: map[string]domain.TransactionState{"tx_bad_commit": domain.TransactionStateActive}}, Effects: &memoryEffectStore{states: map[string]domain.EffectState{}}, Approvals: &memoryApprovalStore{}, Policy: &allowPolicy{}}
	if err := engine.StartCommit(context.Background(), "tx_bad_commit", nil); err == nil {
		t.Fatal("expected wrong-state start commit to fail")
	}
}

type memoryTransactionStore struct {
	states map[string]domain.TransactionState
}

func (m *memoryTransactionStore) Get(ctx context.Context, transactionID string) (domain.TransactionState, error) {
	_ = ctx
	state, ok := m.states[transactionID]
	if !ok {
		return "", errors.New("transaction not found")
	}
	return state, nil
}

func (m *memoryTransactionStore) SetState(ctx context.Context, transactionID string, state domain.TransactionState) error {
	_ = ctx
	m.states[transactionID] = state
	return nil
}

type memoryEffectStore struct {
	states map[string]domain.EffectState
}

func (m *memoryEffectStore) Get(ctx context.Context, effectID string) (domain.EffectState, error) {
	_ = ctx
	state, ok := m.states[effectID]
	if !ok {
		return "", errors.New("effect not found")
	}
	return state, nil
}

func (m *memoryEffectStore) SetState(ctx context.Context, effectID string, state domain.EffectState) error {
	_ = ctx
	m.states[effectID] = state
	return nil
}

type memoryLedger struct {
	records []domain.TransitionRecord
}

func (m *memoryLedger) Append(ctx context.Context, record domain.TransitionRecord) error {
	_ = ctx
	m.records = append(m.records, record)
	return nil
}

type memoryApprovalStore struct {
	refs          map[string]domain.ApprovalSnapshotRef
	invalidations map[string]string
}

func (m *memoryApprovalStore) Get(ctx context.Context, transactionID string) (domain.ApprovalSnapshotRef, error) {
	_ = ctx
	if m.refs == nil {
		return domain.ApprovalSnapshotRef{}, errors.New("approval not found")
	}
	ref, ok := m.refs[transactionID]
	if !ok {
		return domain.ApprovalSnapshotRef{}, errors.New("approval not found")
	}
	return ref, nil
}

func (m *memoryApprovalStore) PutApproved(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef) error {
	_ = ctx
	if m.refs == nil {
		m.refs = map[string]domain.ApprovalSnapshotRef{}
	}
	m.refs[transactionID] = ref
	return nil
}

func (m *memoryApprovalStore) Invalidate(ctx context.Context, transactionID, reason string) error {
	_ = ctx
	if m.invalidations == nil {
		m.invalidations = map[string]string{}
	}
	m.invalidations[transactionID] = reason
	return nil
}

type allowPolicy struct{}

func (allowPolicy) EvaluateCommit(ctx context.Context, transactionID string) error {
	_ = ctx
	_ = transactionID
	return nil
}
