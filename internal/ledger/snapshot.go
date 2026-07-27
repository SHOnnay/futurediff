package ledger

import (
	"errors"

	"github.com/SHOnnay/futurediff/internal/domain"
)

// TransactionSnapshot is a non-secret forensic projection of one durable transaction.
// It deliberately excludes credential source references and secret values.
type TransactionSnapshot struct {
	FormatVersion string                      `json:"format_version"`
	Transaction   domain.Transaction          `json:"transaction"`
	Workspace     domain.Workspace            `json:"workspace"`
	Patch         *domain.Patch               `json:"patch,omitempty"`
	Effects       []domain.ExternalEffect     `json:"effects,omitempty"`
	Rows          map[string][]map[string]any `json:"rows"`
}

func normalizeRows(rows []Row) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := make(map[string]any, len(row))
		for k, v := range row {
			item[k] = v
		}
		out = append(out, item)
	}
	return out
}

func (r *Repository) Snapshot(transactionID string) (TransactionSnapshot, error) {
	tx, err := r.Get(transactionID)
	if err != nil {
		return TransactionSnapshot{}, err
	}
	ws, err := r.Workspace(transactionID)
	if err != nil {
		return TransactionSnapshot{}, err
	}
	snapshot := TransactionSnapshot{FormatVersion: "0.1", Transaction: tx, Workspace: ws, Rows: map[string][]map[string]any{}}
	if patch, patchErr := r.Patch(transactionID); patchErr == nil {
		snapshot.Patch = &patch
	} else if !errors.Is(patchErr, ErrNotFound) {
		return TransactionSnapshot{}, patchErr
	}
	effects, err := r.ExternalEffects(transactionID)
	if err != nil {
		return TransactionSnapshot{}, err
	}
	snapshot.Effects = effects
	queries := []struct {
		name, sql string
		args      []Value
	}{
		{"events", `SELECT sequence,event_id,effect_id,event_type,payload_json,payload_digest,fencing_token,created_at,previous_event_hash,event_hash FROM events WHERE transaction_id=? ORDER BY sequence`, []Value{transactionID}},
		{"verification_runs", `SELECT * FROM verification_runs WHERE transaction_id=? ORDER BY created_at,verification_id`, []Value{transactionID}},
		{"verification_check_results", `SELECT c.* FROM verification_check_results c JOIN verification_runs v ON v.verification_id=c.verification_id WHERE v.transaction_id=? ORDER BY v.created_at,c.ordinal`, []Value{transactionID}},
		{"approvals", `SELECT * FROM approvals WHERE transaction_id=? ORDER BY created_at,approval_id`, []Value{transactionID}},
		{"runtime_executions", `SELECT * FROM runtime_executions WHERE transaction_id=? ORDER BY started_at,execution_id`, []Value{transactionID}},
		{"materialized_repository_refs", `SELECT * FROM materialized_repository_refs WHERE transaction_id=?`, []Value{transactionID}},
		{"effect_attempts", `SELECT * FROM effect_attempts WHERE transaction_id=? ORDER BY started_at,attempt_id`, []Value{transactionID}},
		{"receipts", `SELECT r.* FROM receipts r JOIN effects e ON e.effect_id=r.effect_id WHERE e.transaction_id=? ORDER BY r.created_at,r.receipt_id`, []Value{transactionID}},
		{"credential_access_events", `SELECT event_id,transaction_id,effect_id,adapter_id,credential_id,operation,destination,decision,reason,created_at FROM credential_access_events WHERE transaction_id=? ORDER BY sequence`, []Value{transactionID}},
		{"retention_actions", `SELECT * FROM artifact_retention_actions WHERE transaction_id=? ORDER BY applied_at,action_id`, []Value{transactionID}},
	}
	for _, q := range queries {
		rows, queryErr := r.db.Query(q.sql, q.args...)
		if queryErr != nil {
			return TransactionSnapshot{}, queryErr
		}
		snapshot.Rows[q.name] = normalizeRows(rows)
	}
	return snapshot, nil
}
