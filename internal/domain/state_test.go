package domain

import "testing"

func TestTransactionTransitions(t *testing.T) {
	valid := [][2]TransactionState{{StateActive, StateSealed}, {StateReady, StateCommitting}, {StateNeedsReconciliation, StateReady}}
	for _, pair := range valid {
		if err := ValidateTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("expected valid %v: %v", pair, err)
		}
	}
	if err := ValidateTransition(StateActive, StateCommitted); err == nil {
		t.Fatal("unsafe direct commit transition accepted")
	}
}
