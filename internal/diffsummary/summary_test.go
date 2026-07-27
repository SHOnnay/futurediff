package diffsummary

import (
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"strings"
	"testing"
)

func TestBuildDeterministic(t *testing.T) {
	snap := ledger.TransactionSnapshot{Transaction: domain.Transaction{ID: "tx-1", Mode: "cooperative", Status: domain.StateReady, BaseRevision: "base"}, Patch: &domain.Patch{PatchSHA256: "p", StagedTreeOID: "tree", ChangedPaths: []string{"z.go", "a.go"}}, Effects: []domain.ExternalEffect{{EffectID: "e2", ToolIdentity: "slack.post", AdapterIdentity: "slack", Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 2}, {EffectID: "e1", ToolIdentity: "github.pr", AdapterIdentity: "github", Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 1}}, Rows: map[string][]map[string]any{"verification_runs": {{"outcome": "pass", "verification_digest": "v"}}, "approvals": {}, "receipts": {}, "runtime_executions": {}, "events": {{}}}}
	s, e := Build(snap)
	if e != nil {
		t.Fatal(e)
	}
	if s.ChangedPaths[0] != "a.go" || s.Effects[0].EffectID != "e1" || s.SummaryDigest == "" {
		t.Fatal("not normalized")
	}
	if !strings.Contains(Markdown(s), "Changed paths") {
		t.Fatal("markdown")
	}
}
