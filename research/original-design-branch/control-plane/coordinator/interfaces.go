package coordinator

import (
	"context"

	"github.com/futurediff/futurediff/control-plane/domain"
)

type TransactionStore interface {
	Get(ctx context.Context, transactionID string) (domain.TransactionState, error)
	SetState(ctx context.Context, transactionID string, state domain.TransactionState) error
}

type EffectStore interface {
	Get(ctx context.Context, effectID string) (domain.EffectState, error)
	SetState(ctx context.Context, effectID string, state domain.EffectState) error
}

type LedgerWriter interface {
	Append(ctx context.Context, record domain.TransitionRecord) error
}

type LockManager interface {
	Acquire(ctx context.Context, transactionID string, resourceURIs []string) error
	Renew(ctx context.Context, transactionID string) error
	Release(ctx context.Context, transactionID string) error
}

type ApprovalStore interface {
	Get(ctx context.Context, transactionID string) (domain.ApprovalSnapshotRef, error)
	PutApproved(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef) error
	Invalidate(ctx context.Context, transactionID, reason string) error
}

type PolicyEvaluator interface {
	EvaluateCommit(ctx context.Context, transactionID string) error
}

type Reconciler interface {
	Reconcile(ctx context.Context, transactionID string) error
}

type AdapterRegistry interface {
	Lookup(adapterName string) (AdapterRunner, error)
}

type AdapterRunner interface {
	Describe(ctx context.Context) (any, error)
	Status(ctx context.Context, effectID string) (domain.EffectState, error)
}
