package ledger

import "time"

type MetricsSnapshot struct {
	GeneratedAt            time.Time        `json:"generated_at"`
	TransactionsTotal      int64            `json:"transactions_total"`
	TransactionsByStatus   map[string]int64 `json:"transactions_by_status"`
	EffectsTotal           int64            `json:"effects_total"`
	EffectsByStatus        map[string]int64 `json:"effects_by_status"`
	VerificationRunsTotal  int64            `json:"verification_runs_total"`
	VerificationByOutcome  map[string]int64 `json:"verification_by_outcome"`
	RuntimeExecutionsTotal int64            `json:"runtime_executions_total"`
	RuntimeByTermination   map[string]int64 `json:"runtime_by_termination"`
	CredentialAccessTotal  int64            `json:"credential_access_total"`
	CredentialByDecision   map[string]int64 `json:"credential_by_decision"`
	UnknownEffects         int64            `json:"unknown_effects"`
	UnresolvedTransactions int64            `json:"unresolved_transactions"`
}

func (r *Repository) Metrics() (MetricsSnapshot, error) {
	out := MetricsSnapshot{GeneratedAt: time.Now().UTC(), TransactionsByStatus: map[string]int64{}, EffectsByStatus: map[string]int64{}, VerificationByOutcome: map[string]int64{}, RuntimeByTermination: map[string]int64{}, CredentialByDecision: map[string]int64{}}
	total := func(sql string) (int64, error) {
		row, e := r.db.QueryOne(sql)
		if e != nil {
			return 0, e
		}
		return Int64(row, "n"), nil
	}
	grouped := func(sql, key string, dst map[string]int64) error {
		rows, e := r.db.Query(sql)
		if e != nil {
			return e
		}
		for _, row := range rows {
			dst[String(row, key)] = Int64(row, "n")
		}
		return nil
	}
	var e error
	if out.TransactionsTotal, e = total("SELECT COUNT(*) AS n FROM transactions"); e != nil {
		return out, e
	}
	if e = grouped("SELECT status,COUNT(*) AS n FROM transactions GROUP BY status", "status", out.TransactionsByStatus); e != nil {
		return out, e
	}
	if out.EffectsTotal, e = total("SELECT COUNT(*) AS n FROM effects"); e != nil {
		return out, e
	}
	if e = grouped("SELECT status,COUNT(*) AS n FROM effects GROUP BY status", "status", out.EffectsByStatus); e != nil {
		return out, e
	}
	if out.VerificationRunsTotal, e = total("SELECT COUNT(*) AS n FROM verification_runs"); e != nil {
		return out, e
	}
	if e = grouped("SELECT outcome,COUNT(*) AS n FROM verification_runs GROUP BY outcome", "outcome", out.VerificationByOutcome); e != nil {
		return out, e
	}
	if out.RuntimeExecutionsTotal, e = total("SELECT COUNT(*) AS n FROM runtime_executions"); e != nil {
		return out, e
	}
	if e = grouped("SELECT termination_reason,COUNT(*) AS n FROM runtime_executions GROUP BY termination_reason", "termination_reason", out.RuntimeByTermination); e != nil {
		return out, e
	}
	if out.CredentialAccessTotal, e = total("SELECT COUNT(*) AS n FROM credential_access_events"); e != nil {
		return out, e
	}
	if e = grouped("SELECT decision,COUNT(*) AS n FROM credential_access_events GROUP BY decision", "decision", out.CredentialByDecision); e != nil {
		return out, e
	}
	if out.UnknownEffects, e = total("SELECT COUNT(*) AS n FROM effects WHERE status='unknown'"); e != nil {
		return out, e
	}
	if out.UnresolvedTransactions, e = total("SELECT COUNT(*) AS n FROM transactions WHERE status IN ('committing','compensating','needs_reconciliation','manual_intervention')"); e != nil {
		return out, e
	}
	return out, nil
}
