package ledger

import (
	"fmt"
	"sort"
	"time"
)

type AuditSeverity string

const (
	AuditError   AuditSeverity = "error"
	AuditWarning AuditSeverity = "warning"
)

type AuditFinding struct {
	Code          string        `json:"code"`
	Severity      AuditSeverity `json:"severity"`
	TransactionID string        `json:"transaction_id,omitempty"`
	EffectID      string        `json:"effect_id,omitempty"`
	Message       string        `json:"message"`
}

type AuditReport struct {
	GeneratedAt  time.Time        `json:"generated_at"`
	Healthy      bool             `json:"healthy"`
	Health       Health           `json:"health"`
	EventChain   EventChainReport `json:"event_chain"`
	ErrorCount   int              `json:"error_count"`
	WarningCount int              `json:"warning_count"`
	Findings     []AuditFinding   `json:"findings,omitempty"`
}

func (r *Repository) Audit() (AuditReport, error) {
	report := AuditReport{GeneratedAt: time.Now().UTC(), Healthy: true}
	health, err := r.HealthCheck()
	if err != nil {
		report.Healthy = false
		report.Findings = append(report.Findings, AuditFinding{Code: "sqlite_integrity", Severity: AuditError, Message: err.Error()})
	} else {
		report.Health = health
	}
	chain, chainErr := r.VerifyEventChains()
	report.EventChain = chain
	if chainErr != nil {
		report.Healthy = false
		for _, message := range chain.Findings {
			report.Findings = append(report.Findings, AuditFinding{Code: "event_chain", Severity: AuditError, Message: message})
		}
	}

	checks := []struct {
		query    string
		code     string
		severity AuditSeverity
		message  func(Row) string
	}{
		{
			query: `SELECT t.transaction_id FROM transactions t WHERE t.approval_digest IS NOT NULL AND NOT EXISTS (SELECT 1 FROM approvals a WHERE a.transaction_id=t.transaction_id AND a.transaction_digest=t.approval_digest AND a.material_revision=t.material_revision AND a.decision='approved')`,
			code:  "approval_binding", severity: AuditError,
			message: func(Row) string { return "transaction approval digest has no matching approval record" },
		},
		{
			query: `SELECT t.transaction_id FROM transactions t WHERE t.status='committed' AND NOT EXISTS (SELECT 1 FROM materialized_repository_refs m WHERE m.transaction_id=t.transaction_id)`,
			code:  "committed_without_repository_ref", severity: AuditError,
			message: func(Row) string { return "committed transaction has no materialized repository reference" },
		},
		{
			query: `SELECT e.transaction_id,e.effect_id FROM effects e WHERE e.status='committed' AND NOT EXISTS (SELECT 1 FROM receipts r WHERE r.effect_id=e.effect_id)`,
			code:  "committed_effect_without_receipt", severity: AuditError,
			message: func(Row) string { return "committed effect has no provider receipt" },
		},
		{
			query: `SELECT e.transaction_id,e.effect_id FROM effects e JOIN receipts r ON r.effect_id=e.effect_id WHERE e.status<>'committed'`,
			code:  "receipt_state_mismatch", severity: AuditError,
			message: func(Row) string { return "provider receipt exists for an effect not marked committed" },
		},
		{
			query: `SELECT e.transaction_id,e.effect_id FROM effects e JOIN transactions t ON t.transaction_id=e.transaction_id WHERE e.status='unknown' AND t.status NOT IN ('needs_reconciliation','manual_intervention')`,
			code:  "unknown_effect_without_reconciliation", severity: AuditError,
			message: func(Row) string { return "unknown effect is not protected by reconciliation state" },
		},
		{
			query: `SELECT e.transaction_id,e.effect_id FROM effects e JOIN transactions t ON t.transaction_id=e.transaction_id WHERE t.status IN ('committed','aborted','compensated') AND e.status IN ('preparing','prepared','verified','committing','unknown','aborting','compensating')`,
			code:  "terminal_transaction_with_live_effect", severity: AuditError,
			message: func(Row) string { return "terminal transaction still contains a non-terminal effect" },
		},
		{
			query: `SELECT transaction_id FROM transactions WHERE status IN ('committing','needs_reconciliation','compensating') AND julianday(updated_at) < julianday('now','-24 hours')`,
			code:  "stale_unresolved_transaction", severity: AuditWarning,
			message: func(Row) string { return "transaction has remained unresolved for more than 24 hours" },
		},
	}
	for _, check := range checks {
		rows, err := r.db.Query(check.query)
		if err != nil {
			return report, fmt.Errorf("audit query %s: %w", check.code, err)
		}
		for _, row := range rows {
			finding := AuditFinding{Code: check.code, Severity: check.severity, TransactionID: String(row, "transaction_id"), EffectID: String(row, "effect_id"), Message: check.message(row)}
			report.Findings = append(report.Findings, finding)
		}
	}

	cycleFindings, err := r.auditDependencyCycles()
	if err != nil {
		return report, err
	}
	report.Findings = append(report.Findings, cycleFindings...)
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Severity != report.Findings[j].Severity {
			return report.Findings[i].Severity < report.Findings[j].Severity
		}
		if report.Findings[i].TransactionID != report.Findings[j].TransactionID {
			return report.Findings[i].TransactionID < report.Findings[j].TransactionID
		}
		if report.Findings[i].EffectID != report.Findings[j].EffectID {
			return report.Findings[i].EffectID < report.Findings[j].EffectID
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	for _, finding := range report.Findings {
		if finding.Severity == AuditError {
			report.ErrorCount++
			report.Healthy = false
		} else {
			report.WarningCount++
		}
	}
	return report, nil
}

func (r *Repository) auditDependencyCycles() ([]AuditFinding, error) {
	rows, err := r.db.Query(`SELECT e.transaction_id,d.effect_id,d.depends_on_effect_id FROM effect_dependencies d JOIN effects e ON e.effect_id=d.effect_id ORDER BY e.transaction_id,d.effect_id,d.depends_on_effect_id`)
	if err != nil {
		return nil, err
	}
	byTx := map[string]map[string][]string{}
	for _, row := range rows {
		txID := String(row, "transaction_id")
		if byTx[txID] == nil {
			byTx[txID] = map[string][]string{}
		}
		effectID := String(row, "effect_id")
		byTx[txID][effectID] = append(byTx[txID][effectID], String(row, "depends_on_effect_id"))
	}
	findings := []AuditFinding{}
	for txID, graph := range byTx {
		visiting, visited := map[string]bool{}, map[string]bool{}
		var visit func(string) bool
		visit = func(node string) bool {
			if visiting[node] {
				return true
			}
			if visited[node] {
				return false
			}
			visiting[node] = true
			for _, dep := range graph[node] {
				if visit(dep) {
					return true
				}
			}
			visiting[node] = false
			visited[node] = true
			return false
		}
		for node := range graph {
			if visit(node) {
				findings = append(findings, AuditFinding{Code: "effect_dependency_cycle", Severity: AuditError, TransactionID: txID, EffectID: node, Message: "effect dependency graph contains a cycle"})
				break
			}
		}
	}
	return findings, nil
}
