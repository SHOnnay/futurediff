package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/futurediff/futurediff/control-plane/domain"
)

type Engine struct {
	Transactions TransactionStore
	Effects      EffectStore
	Ledger       LedgerWriter
	Approvals    ApprovalStore
	Policy       PolicyEvaluator
	Now          func() time.Time
}

func (e Engine) Approve(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateAwaitingApproval {
		return fmt.Errorf("transaction %s is not awaiting approval: %s", transactionID, current)
	}
	if err := e.Approvals.PutApproved(ctx, transactionID, ref); err != nil {
		return fmt.Errorf("store approval: %w", err)
	}
	if err := e.Policy.EvaluateCommit(ctx, transactionID); err != nil {
		return fmt.Errorf("evaluate commit policy: %w", err)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateApproved); err != nil {
			return fmt.Errorf("approve effect %s: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateReadyToCommit); err != nil {
		return fmt.Errorf("set ready-to-commit state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateReadyToCommit),
		Reason:        "approval_valid",
		ActorType:     "coordinator.approval",
		At:            e.now(),
	})
}

func (e Engine) InvalidateApproval(ctx context.Context, transactionID, reason string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateAwaitingApproval && current != domain.TransactionStateReadyToCommit {
		return fmt.Errorf("transaction %s cannot invalidate approval from %s", transactionID, current)
	}
	if err := e.Approvals.Invalidate(ctx, transactionID, reason); err != nil {
		return fmt.Errorf("invalidate approval: %w", err)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStatePrepared); err != nil {
			return fmt.Errorf("reset effect %s: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateActive); err != nil {
		return fmt.Errorf("set active state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateActive),
		Reason:        reason,
		ActorType:     "coordinator.approval",
		At:            e.now(),
	})
}

func (e Engine) StartCommit(ctx context.Context, transactionID string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateReadyToCommit {
		return fmt.Errorf("transaction %s is not ready to commit: %s", transactionID, current)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateCommitting); err != nil {
			return fmt.Errorf("mark effect %s committing: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateCommitting); err != nil {
		return fmt.Errorf("set committing state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateCommitting),
		Reason:        "freshness_valid",
		ActorType:     "coordinator.commit",
		At:            e.now(),
	})
}

func (e Engine) MarkCommitted(ctx context.Context, transactionID string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateCommitting && current != domain.TransactionStateReconciling {
		return fmt.Errorf("transaction %s cannot finalize commit from %s", transactionID, current)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateCommitted); err != nil {
			return fmt.Errorf("mark effect %s committed: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateCommitted); err != nil {
		return fmt.Errorf("set committed state: %w", err)
	}
	reason := "commit_plan_complete"
	if current == domain.TransactionStateReconciling {
		reason = "reconciled_committed"
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateCommitted),
		Reason:        reason,
		ActorType:     "coordinator.commit",
		At:            e.now(),
	})
}

func (e Engine) EnterReconciling(ctx context.Context, transactionID, reason string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateCommitting && current != domain.TransactionStateAborting && current != domain.TransactionStateCompensating {
		return fmt.Errorf("transaction %s cannot enter reconciling from %s", transactionID, current)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateUnknown); err != nil {
			return fmt.Errorf("mark effect %s unknown: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateReconciling); err != nil {
		return fmt.Errorf("set reconciling state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateReconciling),
		Reason:        reason,
		ActorType:     "coordinator.reconcile",
		At:            e.now(),
	})
}

func (e Engine) BeginCompensation(ctx context.Context, transactionID, reason string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateCommitting && current != domain.TransactionStateReconciling {
		return fmt.Errorf("transaction %s cannot begin compensation from %s", transactionID, current)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateCompensating); err != nil {
			return fmt.Errorf("mark effect %s compensating: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateCompensating); err != nil {
		return fmt.Errorf("set compensating state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateCompensating),
		Reason:        reason,
		ActorType:     "coordinator.compensation",
		At:            e.now(),
	})
}

func (e Engine) MarkCompensated(ctx context.Context, transactionID string, effectIDs []string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateCompensating && current != domain.TransactionStateReconciling {
		return fmt.Errorf("transaction %s cannot finalize compensation from %s", transactionID, current)
	}
	for _, effectID := range effectIDs {
		if err := e.Effects.SetState(ctx, effectID, domain.EffectStateCompensated); err != nil {
			return fmt.Errorf("mark effect %s compensated: %w", effectID, err)
		}
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateCompensated); err != nil {
		return fmt.Errorf("set compensated state: %w", err)
	}
	reason := "compensation_complete"
	if current == domain.TransactionStateReconciling {
		reason = "reconciled_compensated"
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateCompensated),
		Reason:        reason,
		ActorType:     "coordinator.compensation",
		At:            e.now(),
	})
}

func (e Engine) RequireManualIntervention(ctx context.Context, transactionID, reason string) error {
	current, err := e.Transactions.Get(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("load transaction state: %w", err)
	}
	if current != domain.TransactionStateReconciling && current != domain.TransactionStateCompensating {
		return fmt.Errorf("transaction %s cannot require manual intervention from %s", transactionID, current)
	}
	if err := e.Transactions.SetState(ctx, transactionID, domain.TransactionStateFailedManualIntervention); err != nil {
		return fmt.Errorf("set manual intervention state: %w", err)
	}
	return e.appendLedger(ctx, domain.TransitionRecord{
		TransactionID: transactionID,
		PreviousState: string(current),
		NextState:     string(domain.TransactionStateFailedManualIntervention),
		Reason:        reason,
		ActorType:     "coordinator.reconcile",
		At:            e.now(),
	})
}

func (e Engine) appendLedger(ctx context.Context, record domain.TransitionRecord) error {
	if e.Ledger == nil {
		return nil
	}
	if err := e.Ledger.Append(ctx, record); err != nil {
		return fmt.Errorf("append ledger: %w", err)
	}
	return nil
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
