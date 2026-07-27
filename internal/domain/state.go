package domain

import "fmt"

type TransactionState string

const (
	StateCreated             TransactionState = "created"
	StateActive              TransactionState = "active"
	StateSealed              TransactionState = "sealed"
	StateVerifying           TransactionState = "verifying"
	StateFailedVerification  TransactionState = "failed_verification"
	StateReady               TransactionState = "ready"
	StateStale               TransactionState = "stale"
	StateCommitting          TransactionState = "committing"
	StateAborting            TransactionState = "aborting"
	StateAborted             TransactionState = "aborted"
	StateCompensating        TransactionState = "compensating"
	StateCompensated         TransactionState = "compensated"
	StateNeedsReconciliation TransactionState = "needs_reconciliation"
	StateCommitted           TransactionState = "committed"
	StateManualIntervention  TransactionState = "manual_intervention"
)

var transactionTransitions = map[TransactionState]map[TransactionState]struct{}{
	StateCreated:             set(StateActive, StateAborting),
	StateActive:              set(StateSealed, StateAborting),
	StateSealed:              set(StateVerifying, StateAborting),
	StateVerifying:           set(StateFailedVerification, StateReady, StateStale),
	StateFailedVerification:  set(StateActive, StateAborting),
	StateReady:               set(StateCommitting, StateStale, StateAborting),
	StateStale:               set(StateVerifying, StateAborting),
	StateCommitting:          set(StateCommitted, StateCompensating, StateNeedsReconciliation),
	StateCompensating:        set(StateCompensated, StateNeedsReconciliation),
	StateAborting:            set(StateAborted),
	StateNeedsReconciliation: set(StateReady, StateStale, StateCommitted, StateCompensated, StateManualIntervention),
}

func set(states ...TransactionState) map[TransactionState]struct{} {
	m := make(map[TransactionState]struct{}, len(states))
	for _, s := range states {
		m[s] = struct{}{}
	}
	return m
}

func CanTransition(from, to TransactionState) bool {
	_, ok := transactionTransitions[from][to]
	return ok
}

func ValidateTransition(from, to TransactionState) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("illegal transaction transition %s -> %s", from, to)
	}
	return nil
}

type EffectState string

const (
	EffectDiscovered   EffectState = "discovered"
	EffectPreparing    EffectState = "preparing"
	EffectPrepared     EffectState = "prepared"
	EffectVerified     EffectState = "verified"
	EffectCommitting   EffectState = "committing"
	EffectCommitted    EffectState = "committed"
	EffectUnknown      EffectState = "unknown"
	EffectAborting     EffectState = "aborting"
	EffectAborted      EffectState = "aborted"
	EffectCompensating EffectState = "compensating"
	EffectCompensated  EffectState = "compensated"
	EffectFailed       EffectState = "failed"
	EffectManual       EffectState = "manual"
	EffectSuperseded   EffectState = "superseded"
)
