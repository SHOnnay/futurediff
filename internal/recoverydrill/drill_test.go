package recoverydrill

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"testing"
)

func TestNoBlindRetry(t *testing.T) {
	p, e := Decide(Input{TransactionStatus: domain.StateNeedsReconciliation, EffectStatus: domain.EffectUnknown, ProviderStatus: "unknown"})
	if e != nil {
		t.Fatal(e)
	}
	if p.Action != "query_status" || p.BlindRetryAllowed {
		t.Fatalf("unsafe plan %+v", p)
	}
}
func TestSelfTest(t *testing.T) {
	if !SelfTest().Passed {
		t.Fatal("self test failed")
	}
}
