package metrics

import (
	"fmt"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"sort"
	"strings"
)

func Prometheus(s ledger.MetricsSnapshot) string {
	var b strings.Builder
	scalar := func(name string, v int64) { fmt.Fprintf(&b, "# TYPE %s gauge\n%s %d\n", name, name, v) }
	scalar("futurediff_transactions_total", s.TransactionsTotal)
	scalar("futurediff_effects_total", s.EffectsTotal)
	scalar("futurediff_verification_runs_total", s.VerificationRunsTotal)
	scalar("futurediff_runtime_executions_total", s.RuntimeExecutionsTotal)
	scalar("futurediff_credential_access_total", s.CredentialAccessTotal)
	scalar("futurediff_unknown_effects", s.UnknownEffects)
	scalar("futurediff_unresolved_transactions", s.UnresolvedTransactions)
	maps := []struct {
		name, label string
		m           map[string]int64
	}{{"futurediff_transactions_by_status", "status", s.TransactionsByStatus}, {"futurediff_effects_by_status", "status", s.EffectsByStatus}, {"futurediff_verification_by_outcome", "outcome", s.VerificationByOutcome}, {"futurediff_runtime_by_termination", "reason", s.RuntimeByTermination}, {"futurediff_credential_by_decision", "decision", s.CredentialByDecision}}
	for _, x := range maps {
		keys := make([]string, 0, len(x.m))
		for k := range x.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "# TYPE %s gauge\n", x.name)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s{%s=%q} %d\n", x.name, x.label, k, x.m[k])
		}
	}
	return b.String()
}
