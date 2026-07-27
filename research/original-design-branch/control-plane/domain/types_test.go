package domain

import "testing"

func TestTransactionStatesAreNonEmpty(t *testing.T) {
	states := []TransactionState{
		TransactionStateNew,
		TransactionStateActive,
		TransactionStateVerifying,
		TransactionStateAwaitingApproval,
		TransactionStateReadyToCommit,
		TransactionStateCommitting,
		TransactionStateCommitted,
		TransactionStateAborting,
		TransactionStateAborted,
		TransactionStateReconciling,
		TransactionStateCompensating,
		TransactionStateCompensated,
		TransactionStateFailedManualIntervention,
	}

	for _, state := range states {
		if state == "" {
			t.Fatal("transaction state must not be empty")
		}
	}
}

func TestEffectStatesAreNonEmpty(t *testing.T) {
	states := []EffectState{
		EffectStateDeclared,
		EffectStatePrepared,
		EffectStatePreviewed,
		EffectStateVerified,
		EffectStateApproved,
		EffectStateCommitting,
		EffectStateCommitted,
		EffectStateAborted,
		EffectStateCompensating,
		EffectStateCompensated,
		EffectStateUnknown,
	}

	for _, state := range states {
		if state == "" {
			t.Fatal("effect state must not be empty")
		}
	}
}
