package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

// apiAccessHashMaterial is the canonical, payload-free material protected by
// the API access hash chain. Sequence is included so row reordering is visible.
type apiAccessHashMaterial struct {
	Sequence             int64  `json:"sequence"`
	EventID              string `json:"event_id"`
	PrincipalID          string `json:"principal_id"`
	Method               string `json:"method"`
	Path                 string `json:"path"`
	StatusCode           int    `json:"status_code"`
	IdempotencyKeyDigest string `json:"idempotency_key_digest,omitempty"`
	RequestDigest        string `json:"request_digest,omitempty"`
	RequestID            string `json:"request_id,omitempty"`
	CreatedAt            string `json:"created_at"`
	PreviousDigest       string `json:"previous_digest,omitempty"`
}

func apiAccessDigest(m apiAccessHashMaterial) string {
	data, _ := json.Marshal(m)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type APIRequestRecord struct {
	PrincipalID         string
	IdempotencyKey      string
	Method              string
	Path                string
	RequestDigest       string
	State               string
	StatusCode          int
	ResponseContentType string
	ResponseBody        []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (r *Repository) BeginAPIRequest(principal, key, method, path, digest string) (APIRequestRecord, bool, error) {
	if principal == "" || key == "" || method == "" || path == "" || digest == "" {
		return APIRequestRecord{}, false, errors.New("principal, key, method, path and digest are required")
	}
	now := time.Now().UTC()
	var existing APIRequestRecord
	created := false
	err := r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query(`SELECT * FROM api_idempotency_requests WHERE principal_id=? AND idempotency_key=?`, principal, key)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			existing = apiRequestFromRow(rows[0])
			return nil
		}
		_, err = tx.Exec(`INSERT INTO api_idempotency_requests(principal_id,idempotency_key,method,path,request_digest,state,created_at,updated_at) VALUES(?,?,?,?,?,'in_progress',?,?)`, principal, key, method, path, digest, ts(now), ts(now))
		if err != nil {
			return err
		}
		created = true
		existing = APIRequestRecord{PrincipalID: principal, IdempotencyKey: key, Method: method, Path: path, RequestDigest: digest, State: "in_progress", CreatedAt: now, UpdatedAt: now}
		return nil
	})
	return existing, created, err
}

func (r *Repository) CompleteAPIRequest(principal, key, digest string, status int, contentType string, body []byte) error {
	if len(body) > 1<<20 {
		return errors.New("idempotency response exceeds 1 MiB")
	}
	now := time.Now().UTC()
	changes, err := r.db.Exec(`UPDATE api_idempotency_requests SET state='completed',status_code=?,response_content_type=?,response_body=?,updated_at=? WHERE principal_id=? AND idempotency_key=? AND request_digest=? AND state='in_progress'`, int64(status), contentType, body, ts(now), principal, key, digest)
	if err != nil {
		return err
	}
	if changes != 1 {
		return errors.New("idempotency reservation was not active")
	}
	return nil
}

func (r *Repository) AbortAPIRequest(principal, key, digest string) error {
	_, err := r.db.Exec(`DELETE FROM api_idempotency_requests WHERE principal_id=? AND idempotency_key=? AND request_digest=? AND state='in_progress'`, principal, key, digest)
	return err
}

func (r *Repository) RecordAPIAccess(principal, method, path string, status int, keyDigest, requestDigest, requestID string) error {
	now := time.Now().UTC()
	eventID := domain.NewID("api")
	return r.db.WithTx(func(tx *Tx) error {
		previous := ""
		rows, err := tx.Query(`SELECT event_digest FROM api_access_events ORDER BY sequence DESC LIMIT 1`)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			previous = String(rows[0], "event_digest")
		}
		seqRow, err := tx.QueryOne(`SELECT COALESCE(MAX(sequence),0)+1 AS next_sequence FROM api_access_events`)
		if err != nil {
			return err
		}
		sequence := Int64(seqRow, "next_sequence")
		material := apiAccessHashMaterial{Sequence: sequence, EventID: eventID, PrincipalID: principal, Method: method, Path: path, StatusCode: status, IdempotencyKeyDigest: keyDigest, RequestDigest: requestDigest, RequestID: requestID, CreatedAt: ts(now), PreviousDigest: previous}
		digest := apiAccessDigest(material)
		_, err = tx.Exec(`INSERT INTO api_access_events(sequence,event_id,principal_id,method,path,status_code,idempotency_key_digest,request_digest,request_id,created_at,previous_digest,event_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sequence, eventID, principal, method, path, int64(status), nullString(keyDigest), nullString(requestDigest), nullString(requestID), ts(now), nullString(previous), digest)
		return err
	})
}

func (r *Repository) APIAccessCount() (int64, error) {
	row, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM api_access_events`)
	if err != nil {
		return 0, err
	}
	return Int64(row, "n"), nil
}

func apiRequestFromRow(row Row) APIRequestRecord {
	createdAt, _ := parseTime(String(row, "created_at"))
	updatedAt, _ := parseTime(String(row, "updated_at"))
	record := APIRequestRecord{PrincipalID: String(row, "principal_id"), IdempotencyKey: String(row, "idempotency_key"), Method: String(row, "method"), Path: String(row, "path"), RequestDigest: String(row, "request_digest"), State: String(row, "state"), StatusCode: int(Int64(row, "status_code")), ResponseContentType: String(row, "response_content_type"), CreatedAt: createdAt, UpdatedAt: updatedAt}
	if value, ok := row["response_body"].([]byte); ok {
		record.ResponseBody = append([]byte(nil), value...)
	} else if value := row["response_body"]; value != nil {
		record.ResponseBody = []byte(fmt.Sprint(value))
	}
	return record
}

type APIAccessEvent struct {
	Sequence             int64     `json:"sequence"`
	EventID              string    `json:"event_id"`
	PrincipalID          string    `json:"principal_id"`
	Method               string    `json:"method"`
	Path                 string    `json:"path"`
	StatusCode           int       `json:"status_code"`
	IdempotencyKeyDigest string    `json:"idempotency_key_digest,omitempty"`
	RequestDigest        string    `json:"request_digest,omitempty"`
	RequestID            string    `json:"request_id,omitempty"`
	PreviousDigest       string    `json:"previous_digest,omitempty"`
	EventDigest          string    `json:"event_digest"`
	CreatedAt            time.Time `json:"created_at"`
}

type APIAccessSummary struct {
	Total      int64            `json:"total"`
	ByStatus   map[string]int64 `json:"by_status"`
	Recent     []APIAccessEvent `json:"recent"`
	ChainValid bool             `json:"chain_valid"`
	ChainError string           `json:"chain_error,omitempty"`
	HeadDigest string           `json:"head_digest,omitempty"`
}

func apiAccessEventFromRow(row Row) APIAccessEvent {
	created, _ := parseTime(String(row, "created_at"))
	return APIAccessEvent{Sequence: Int64(row, "sequence"), EventID: String(row, "event_id"), PrincipalID: String(row, "principal_id"), Method: String(row, "method"), Path: String(row, "path"), StatusCode: int(Int64(row, "status_code")), IdempotencyKeyDigest: String(row, "idempotency_key_digest"), RequestDigest: String(row, "request_digest"), RequestID: String(row, "request_id"), PreviousDigest: String(row, "previous_digest"), EventDigest: String(row, "event_digest"), CreatedAt: created}
}

func (r *Repository) VerifyAPIAccessChain() (string, error) {
	rows, err := r.db.Query(`SELECT sequence,event_id,principal_id,method,path,status_code,idempotency_key_digest,request_digest,request_id,created_at,previous_digest,event_digest FROM api_access_events ORDER BY sequence`)
	if err != nil {
		return "", err
	}
	previous := ""
	var expectedSequence int64 = 1
	for _, row := range rows {
		event := apiAccessEventFromRow(row)
		if event.Sequence != expectedSequence {
			return previous, fmt.Errorf("API access sequence gap: expected %d found %d", expectedSequence, event.Sequence)
		}
		if event.PreviousDigest != previous {
			return previous, fmt.Errorf("API access previous digest mismatch at sequence %d", event.Sequence)
		}
		material := apiAccessHashMaterial{Sequence: event.Sequence, EventID: event.EventID, PrincipalID: event.PrincipalID, Method: event.Method, Path: event.Path, StatusCode: event.StatusCode, IdempotencyKeyDigest: event.IdempotencyKeyDigest, RequestDigest: event.RequestDigest, RequestID: event.RequestID, CreatedAt: ts(event.CreatedAt), PreviousDigest: event.PreviousDigest}
		calculated := apiAccessDigest(material)
		if event.EventDigest == "" || event.EventDigest != calculated {
			return previous, fmt.Errorf("API access event digest mismatch at sequence %d", event.Sequence)
		}
		previous = event.EventDigest
		expectedSequence++
	}
	return previous, nil
}

func (r *Repository) backfillAPIAccessChain() error {
	return r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query(`SELECT sequence,event_id,principal_id,method,path,status_code,idempotency_key_digest,request_digest,request_id,created_at,previous_digest,event_digest FROM api_access_events ORDER BY sequence`)
		if err != nil {
			return err
		}
		previous := ""
		for _, row := range rows {
			event := apiAccessEventFromRow(row)
			material := apiAccessHashMaterial{Sequence: event.Sequence, EventID: event.EventID, PrincipalID: event.PrincipalID, Method: event.Method, Path: event.Path, StatusCode: event.StatusCode, IdempotencyKeyDigest: event.IdempotencyKeyDigest, RequestDigest: event.RequestDigest, RequestID: event.RequestID, CreatedAt: ts(event.CreatedAt), PreviousDigest: previous}
			digest := apiAccessDigest(material)
			if event.PreviousDigest != previous || event.EventDigest != digest {
				if event.EventDigest != "" {
					return fmt.Errorf("existing API access chain is invalid at sequence %d", event.Sequence)
				}
				if _, err := tx.Exec(`UPDATE api_access_events SET previous_digest=?,event_digest=? WHERE sequence=?`, nullString(previous), digest, event.Sequence); err != nil {
					return err
				}
			}
			previous = digest
		}
		return nil
	})
}

func (r *Repository) APIAccessSummary(limit int) (APIAccessSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.Query(`SELECT sequence,event_id,principal_id,method,path,status_code,idempotency_key_digest,request_digest,request_id,created_at,previous_digest,event_digest FROM api_access_events ORDER BY sequence DESC LIMIT ?`, int64(limit))
	if err != nil {
		return APIAccessSummary{}, err
	}
	summary := APIAccessSummary{ByStatus: map[string]int64{}, Recent: make([]APIAccessEvent, 0, len(rows))}
	countRow, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM api_access_events`)
	if err != nil {
		return APIAccessSummary{}, err
	}
	summary.Total = Int64(countRow, "n")
	statusRows, err := r.db.Query(`SELECT status_code,COUNT(*) AS n FROM api_access_events GROUP BY status_code ORDER BY status_code`)
	if err != nil {
		return APIAccessSummary{}, err
	}
	for _, row := range statusRows {
		summary.ByStatus[fmt.Sprint(Int64(row, "status_code"))] = Int64(row, "n")
	}
	for _, row := range rows {
		summary.Recent = append(summary.Recent, apiAccessEventFromRow(row))
	}
	head, verifyErr := r.VerifyAPIAccessChain()
	summary.HeadDigest = head
	summary.ChainValid = verifyErr == nil
	if verifyErr != nil {
		summary.ChainError = verifyErr.Error()
	}
	return summary, nil
}
