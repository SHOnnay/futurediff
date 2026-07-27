package ledger

import (
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type RetentionRecord struct {
	ActionID      string    `json:"action_id"`
	TransactionID string    `json:"transaction_id"`
	RuntimeRoot   string    `json:"runtime_root"`
	BytesRemoved  int64     `json:"bytes_removed"`
	PlanDigest    string    `json:"plan_digest"`
	AppliedAt     time.Time `json:"applied_at"`
}

func (r *Repository) TerminalWorkspaces(before time.Time) ([]domain.Workspace, error) {
	rows, err := r.db.Query(`SELECT w.* FROM transaction_workspaces w JOIN transactions t ON t.transaction_id=w.transaction_id LEFT JOIN artifact_retention_actions a ON a.transaction_id=w.transaction_id WHERE t.status IN ('committed','aborted','compensated') AND t.updated_at < ? AND a.transaction_id IS NULL ORDER BY t.updated_at,w.transaction_id`, ts(before))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Workspace, 0, len(rows))
	for _, row := range rows {
		workspace, err := workspaceFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, workspace)
	}
	return out, nil
}

func (r *Repository) RecordRetention(record RetentionRecord) error {
	if record.ActionID == "" {
		record.ActionID = domain.NewID("retention")
	}
	if record.AppliedAt.IsZero() {
		record.AppliedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO artifact_retention_actions(action_id,transaction_id,runtime_root,bytes_removed,plan_digest,applied_at) VALUES(?,?,?,?,?,?)`, record.ActionID, record.TransactionID, record.RuntimeRoot, record.BytesRemoved, record.PlanDigest, ts(record.AppliedAt))
	return err
}
