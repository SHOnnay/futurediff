package ledger

import (
	"github.com/SHOnnay/futurediff/internal/domain"
)

// TransactionsWithEffects returns every transaction that has at least one
// prepared external effect (superseded effects are excluded from the durable
// view, matching ExternalEffects), newest first. Read-only: it is used by the
// restore comparison and never mutates the ledger.
func (r *Repository) TransactionsWithEffects() ([]domain.Transaction, error) {
	rows, err := r.db.Query(`SELECT DISTINCT t.* FROM transactions t JOIN effects e ON e.transaction_id = t.transaction_id AND e.status <> 'superseded' ORDER BY t.created_at DESC, t.transaction_id`)
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

// EffectAttempts returns the durable write-ahead attempts recorded for a
// transaction's effects, oldest first (started_at, then attempt_id). It is
// the read-only counterpart of BeginEffectAttempt and is used by the restore
// comparison to distinguish effects that were never dispatched from effects
// that were attempted and proved absent or were left ambiguous.
func (r *Repository) EffectAttempts(transactionID string) ([]domain.EffectAttempt, error) {
	rows, err := r.db.Query(`SELECT attempt_id,effect_id,transaction_id,phase,request_digest,fencing_token,outcome,http_status,response_digest,error_class,error_message,started_at,finished_at FROM effect_attempts WHERE transaction_id=? ORDER BY started_at, attempt_id`, transactionID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.EffectAttempt, 0, len(rows))
	for _, row := range rows {
		attempt, err := effectAttemptFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, nil
}

func effectAttemptFromRow(row Row) (domain.EffectAttempt, error) {
	started, err := parseTime(String(row, "started_at"))
	if err != nil {
		return domain.EffectAttempt{}, err
	}
	attempt := domain.EffectAttempt{
		AttemptID:     String(row, "attempt_id"),
		EffectID:      String(row, "effect_id"),
		TransactionID: String(row, "transaction_id"),
		Phase:         String(row, "phase"),
		RequestDigest: String(row, "request_digest"),
		FencingToken:  Int64(row, "fencing_token"),
		Outcome:       String(row, "outcome"),
		StartedAt:     started,
	}
	if v, ok := row["http_status"]; ok && v != nil {
		attempt.HTTPStatus = int(Int64(row, "http_status"))
	}
	attempt.ResponseDigest = String(row, "response_digest")
	attempt.ErrorClass = String(row, "error_class")
	attempt.ErrorMessage = String(row, "error_message")
	if v, ok := row["finished_at"]; ok && v != nil {
		finished, parseErr := parseTime(String(row, "finished_at"))
		if parseErr != nil {
			return domain.EffectAttempt{}, parseErr
		}
		attempt.FinishedAt = finished
	}
	return attempt, nil
}
