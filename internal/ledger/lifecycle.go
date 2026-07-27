package ledger

import (
	"errors"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type ExpiryAction struct {
	ActionID      string                  `json:"action_id"`
	TransactionID string                  `json:"transaction_id"`
	PriorStatus   domain.TransactionState `json:"prior_status"`
	PolicyDigest  string                  `json:"policy_digest"`
	AppliedAt     time.Time               `json:"applied_at"`
}

func (r *Repository) TransactionsByStatusBefore(status domain.TransactionState, before time.Time) ([]domain.Transaction, error) {
	rows, err := r.db.Query(`SELECT * FROM transactions WHERE status=? AND updated_at<? ORDER BY updated_at,transaction_id`, string(status), ts(before.UTC()))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Transaction, 0, len(rows))
	for _, row := range rows {
		tx, err := transactionFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, tx)
	}
	return out, nil
}

func (r *Repository) RecordExpiryAction(a ExpiryAction) error {
	if a.ActionID == "" {
		a.ActionID = domain.NewID("expiry")
	}
	if a.TransactionID == "" || a.PriorStatus == "" || a.PolicyDigest == "" {
		return errors.New("expiry action is incomplete")
	}
	if a.AppliedAt.IsZero() {
		a.AppliedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO transaction_expiry_actions(action_id,transaction_id,prior_status,policy_digest,applied_at) VALUES(?,?,?,?,?)`, a.ActionID, a.TransactionID, string(a.PriorStatus), a.PolicyDigest, ts(a.AppliedAt))
	return err
}

type IdempotencyGCCandidate struct {
	PrincipalID     string    `json:"-"`
	IdempotencyKey  string    `json:"-"`
	PrincipalDigest string    `json:"principal_digest"`
	KeyDigest       string    `json:"key_digest"`
	State           string    `json:"state"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (r *Repository) IdempotencyBefore(state string, before time.Time) ([]APIRequestRecord, error) {
	rows, err := r.db.Query(`SELECT * FROM api_idempotency_requests WHERE state=? AND updated_at<? ORDER BY updated_at,principal_id,idempotency_key`, state, ts(before.UTC()))
	if err != nil {
		return nil, err
	}
	out := make([]APIRequestRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiRequestFromRow(row))
	}
	return out, nil
}

func (r *Repository) DeleteIdempotencyRecords(records []APIRequestRecord) (completed, inProgress int64, err error) {
	err = r.db.WithTx(func(tx *Tx) error {
		for _, rec := range records {
			n, e := tx.Exec(`DELETE FROM api_idempotency_requests WHERE principal_id=? AND idempotency_key=? AND request_digest=? AND state=? AND updated_at=?`, rec.PrincipalID, rec.IdempotencyKey, rec.RequestDigest, rec.State, ts(rec.UpdatedAt))
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("idempotency record changed after planning")
			}
			if rec.State == "completed" {
				completed++
			} else if rec.State == "in_progress" {
				inProgress++
			}
		}
		return nil
	})
	return
}

func (r *Repository) RecordIdempotencyGC(actionID string, completed, inProgress int64, completedBefore, inProgressBefore time.Time, planDigest string, appliedAt time.Time) error {
	if actionID == "" {
		actionID = domain.NewID("idgc")
	}
	if appliedAt.IsZero() {
		appliedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO idempotency_gc_actions(action_id,completed_deleted,in_progress_deleted,completed_before,in_progress_before,plan_digest,applied_at) VALUES(?,?,?,?,?,?,?)`, actionID, completed, inProgress, ts(completedBefore.UTC()), ts(inProgressBefore.UTC()), planDigest, ts(appliedAt))
	return err
}

func (r *Repository) Backups() ([]BackupRecord, error) {
	rows, err := r.db.Query(`SELECT backup_id,path,sha256,size_bytes,created_at FROM ledger_backups ORDER BY created_at DESC,backup_id DESC`)
	if err != nil {
		return nil, err
	}
	out := make([]BackupRecord, 0, len(rows))
	for _, row := range rows {
		created, _ := parseTime(String(row, "created_at"))
		out = append(out, BackupRecord{BackupID: String(row, "backup_id"), Path: String(row, "path"), SHA256: String(row, "sha256"), SizeBytes: Int64(row, "size_bytes"), CreatedAt: created})
	}
	return out, nil
}

func (r *Repository) DeleteBackupRecord(backupID, expectedSHA string) error {
	n, err := r.db.Exec(`DELETE FROM ledger_backups WHERE backup_id=? AND sha256=?`, backupID, expectedSHA)
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("backup record changed after planning")
	}
	return nil
}

func (r *Repository) RecordBackupRetention(actionID, backupID, pathDigest string, bytesRemoved int64, planDigest string, appliedAt time.Time) error {
	if actionID == "" {
		actionID = domain.NewID("backupret")
	}
	if appliedAt.IsZero() {
		appliedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO backup_retention_actions(action_id,backup_id,path_digest,bytes_removed,plan_digest,applied_at) VALUES(?,?,?,?,?,?)`, actionID, backupID, pathDigest, bytesRemoved, planDigest, ts(appliedAt))
	return err
}
