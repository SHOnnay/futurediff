package effectgraph

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"strings"
	"testing"
)

func TestGraph(t *testing.T) {
	s := ledger.TransactionSnapshot{Transaction: domain.Transaction{ID: "tx1", Status: domain.StateReady}, Rows: map[string][]map[string]any{"verification_runs": {{"outcome": "pass"}}, "approvals": {{"decision": "approved"}}}, Effects: []domain.ExternalEffect{{EffectID: "e1", Operation: "github.create", Destination: "https://api.github.com", Status: domain.EffectPrepared}}}
	g := FromSnapshot(s)
	if g.Digest == "" || len(g.Nodes) < 4 {
		t.Fatalf("%+v", g)
	}
	if !strings.Contains(Mermaid(g), "effect_e1") {
		t.Fatal("missing effect")
	}
}
