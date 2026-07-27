package domain

import "time"

type TransactionState string

type EffectState string

const (
	TransactionStateNew                      TransactionState = "NEW"
	TransactionStateActive                   TransactionState = "ACTIVE"
	TransactionStateVerifying                TransactionState = "VERIFYING"
	TransactionStateAwaitingApproval         TransactionState = "AWAITING_APPROVAL"
	TransactionStateReadyToCommit            TransactionState = "READY_TO_COMMIT"
	TransactionStateCommitting               TransactionState = "COMMITTING"
	TransactionStateCommitted                TransactionState = "COMMITTED"
	TransactionStateAborting                 TransactionState = "ABORTING"
	TransactionStateAborted                  TransactionState = "ABORTED"
	TransactionStateReconciling              TransactionState = "RECONCILING"
	TransactionStateCompensating             TransactionState = "COMPENSATING"
	TransactionStateCompensated              TransactionState = "COMPENSATED"
	TransactionStateFailedManualIntervention TransactionState = "FAILED_MANUAL_INTERVENTION"
)

const (
	EffectStateDeclared     EffectState = "DECLARED"
	EffectStatePrepared     EffectState = "PREPARED"
	EffectStatePreviewed    EffectState = "PREVIEWED"
	EffectStateVerified     EffectState = "VERIFIED"
	EffectStateApproved     EffectState = "APPROVED"
	EffectStateCommitting   EffectState = "COMMITTING"
	EffectStateCommitted    EffectState = "COMMITTED"
	EffectStateAborted      EffectState = "ABORTED"
	EffectStateCompensating EffectState = "COMPENSATING"
	EffectStateCompensated  EffectState = "COMPENSATED"
	EffectStateUnknown      EffectState = "UNKNOWN"
)

type TransitionRecord struct {
	TransactionID string
	EffectID      string
	PreviousState string
	NextState     string
	Reason        string
	ActorType     string
	AttemptNumber int
	EvidenceRef   string
	At            time.Time
}

type EffectBinding struct {
	EffectID             string
	AdapterName          string
	SupportLevel         string
	PreparedFingerprint  string
	PreviewFingerprint   string
	ResourceURIs         []string
	ResourceVersionPairs []ResourceVersion
}

type ResourceVersion struct {
	ResourceURI string
	Version     string
}

type ApprovalSnapshotRef struct {
	SnapshotID string
	Version    string
	Hash       string
}

type ResourceLock struct {
	TransactionID  string
	ResourceURI    string
	LeaseOwner     string
	LeaseExpiresAt time.Time
}

type ReceiptRef struct {
	AdapterName string
	EffectID    string
	URI         string
}

type RetryBudget struct {
	OperationRetries      int
	StatusPollBudget      int
	CompensationRetries   int
	ManualInterventionTTL time.Duration
}
