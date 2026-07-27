package domain

import "testing"

func TestTransactionTransitionGraphHasNoTerminalOutgoingEdges(t *testing.T) {
	terminal := []TransactionState{StateCommitted, StateAborted, StateCompensated, StateManualIntervention}
	all := []TransactionState{StateCreated, StateActive, StateSealed, StateVerifying, StateFailedVerification, StateReady, StateStale, StateCommitting, StateAborting, StateAborted, StateCompensating, StateCompensated, StateNeedsReconciliation, StateCommitted, StateManualIntervention}
	for _, from := range terminal {
		for _, to := range all {
			if CanTransition(from, to) {
				t.Fatalf("terminal state %s has outgoing transition to %s", from, to)
			}
		}
	}
}

func TestEveryNonterminalTransactionStateHasAnExit(t *testing.T) {
	nonterminal := []TransactionState{StateCreated, StateActive, StateSealed, StateVerifying, StateFailedVerification, StateReady, StateStale, StateCommitting, StateAborting, StateCompensating, StateNeedsReconciliation}
	all := []TransactionState{StateCreated, StateActive, StateSealed, StateVerifying, StateFailedVerification, StateReady, StateStale, StateCommitting, StateAborting, StateAborted, StateCompensating, StateCompensated, StateNeedsReconciliation, StateCommitted, StateManualIntervention}
	for _, from := range nonterminal {
		found := false
		for _, to := range all {
			if CanTransition(from, to) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("state %s has no exit", from)
		}
	}
}
