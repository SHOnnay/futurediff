package agentbench

import (
	"testing"
	"time"
)

func TestBuildOverhead(t *testing.T) {
	now := time.Now()
	runs := []Run{{FormatVersion: "0.1", RunID: "a", Agent: "x", TaskID: "t", Mode: "direct", StartedAt: now, FinishedAt: now.Add(time.Second), InputTokens: 80, OutputTokens: 20, Success: true}, {FormatVersion: "0.1", RunID: "b", Agent: "x", TaskID: "t", Mode: "futurediff", StartedAt: now, FinishedAt: now.Add(1500 * time.Millisecond), InputTokens: 88, OutputTokens: 22, Success: true}}
	r := Build(runs, "direct")
	for _, a := range r.Aggregates {
		if a.Mode == "futurediff" {
			if a.TokenOverheadVsBaselinePct == nil || *a.TokenOverheadVsBaselinePct < 9.9 || *a.TokenOverheadVsBaselinePct > 10.1 {
				t.Fatalf("unexpected overhead: %#v", a.TokenOverheadVsBaselinePct)
			}
			return
		}
	}
	t.Fatal("aggregate missing")
}
func TestRejectNegative(t *testing.T) {
	r := Run{FormatVersion: "0.1", RunID: "a", Agent: "x", TaskID: "t", Mode: "m", StartedAt: time.Now(), FinishedAt: time.Now(), InputTokens: -1}
	if r.Validate() == nil {
		t.Fatal("negative tokens accepted")
	}
}
