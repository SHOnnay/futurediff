package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type TransactionAccess string

const (
	AccessRead    TransactionAccess = "read"
	AccessOperate TransactionAccess = "operate"
	AccessAdmin   TransactionAccess = "admin"
)

type TransactionGrant struct {
	TransactionID string            `json:"transaction_id"`
	PrincipalID   string            `json:"principal_id"`
	Permission    TransactionAccess `json:"permission"`
	GrantedBy     string            `json:"granted_by"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type TransactionAccessEvent struct {
	Sequence           int64             `json:"sequence"`
	EventID            string            `json:"event_id"`
	TransactionID      string            `json:"transaction_id"`
	ActorPrincipalID   string            `json:"actor_principal_id"`
	SubjectPrincipalID string            `json:"subject_principal_id"`
	Action             string            `json:"action"`
	Permission         TransactionAccess `json:"permission,omitempty"`
	RequestID          string            `json:"request_id,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	PreviousDigest     string            `json:"previous_digest,omitempty"`
	EventDigest        string            `json:"event_digest"`
}

type accessHashMaterial struct {
	Sequence           int64  `json:"sequence"`
	EventID            string `json:"event_id"`
	TransactionID      string `json:"transaction_id"`
	ActorPrincipalID   string `json:"actor_principal_id"`
	SubjectPrincipalID string `json:"subject_principal_id"`
	Action             string `json:"action"`
	Permission         string `json:"permission,omitempty"`
	RequestID          string `json:"request_id,omitempty"`
	CreatedAt          string `json:"created_at"`
	PreviousDigest     string `json:"previous_digest,omitempty"`
}

func accessDigest(m accessHashMaterial) string {
	b, _ := json.Marshal(m)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func validGrantPermission(p TransactionAccess) bool { return p == AccessRead || p == AccessOperate }

func appendTransactionAccessEvent(tx *Tx, transactionID, actor, subject, action string, permission TransactionAccess, requestID string, now time.Time) error {
	if transactionID == "" || actor == "" || subject == "" {
		return errors.New("transaction access event identity fields are required")
	}
	rows, err := tx.Query(`SELECT event_digest FROM transaction_access_events ORDER BY sequence DESC LIMIT 1`)
	if err != nil {
		return err
	}
	previous := ""
	if len(rows) > 0 {
		previous = String(rows[0], "event_digest")
	}
	seqRow, err := tx.QueryOne(`SELECT COALESCE(MAX(sequence),0)+1 AS next_sequence FROM transaction_access_events`)
	if err != nil {
		return err
	}
	seq := Int64(seqRow, "next_sequence")
	eventID := domain.NewID("access")
	m := accessHashMaterial{Sequence: seq, EventID: eventID, TransactionID: transactionID, ActorPrincipalID: actor, SubjectPrincipalID: subject, Action: action, Permission: string(permission), RequestID: requestID, CreatedAt: ts(now), PreviousDigest: previous}
	digest := accessDigest(m)
	_, err = tx.Exec(`INSERT INTO transaction_access_events(sequence,event_id,transaction_id,actor_principal_id,subject_principal_id,action,permission,request_id,created_at,previous_digest,event_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, seq, eventID, transactionID, actor, subject, action, nullString(string(permission)), nullString(requestID), ts(now), nullString(previous), digest)
	return err
}

func (r *Repository) RecordTransactionOwner(transactionID, principalID, requestID string) error {
	if principalID == "" {
		return errors.New("owner principal is required")
	}
	return r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne(`SELECT owner_principal_id FROM transactions WHERE transaction_id=?`, transactionID)
		if err != nil {
			return err
		}
		if String(row, "owner_principal_id") != principalID {
			return errors.New("transaction owner mismatch")
		}
		return appendTransactionAccessEvent(tx, transactionID, principalID, principalID, "created", AccessAdmin, requestID, time.Now().UTC())
	})
}

func (r *Repository) CheckTransactionAccess(transactionID, principalID string, required TransactionAccess) (bool, error) {
	row, err := r.db.QueryOne(`SELECT owner_principal_id FROM transactions WHERE transaction_id=?`, transactionID)
	if err != nil {
		return false, err
	}
	if String(row, "owner_principal_id") == principalID {
		return true, nil
	}
	if required == AccessAdmin {
		return false, nil
	}
	rows, err := r.db.Query(`SELECT permission FROM transaction_access_grants WHERE transaction_id=? AND principal_id=?`, transactionID, principalID)
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	permission := TransactionAccess(String(rows[0], "permission"))
	if required == AccessRead {
		return permission == AccessRead || permission == AccessOperate, nil
	}
	return permission == AccessOperate, nil
}

func (r *Repository) ListTransactionsForPrincipal(principalID string, all bool) ([]domain.Transaction, error) {
	var rows []Row
	var err error
	if all {
		rows, err = r.db.Query(`SELECT * FROM transactions ORDER BY updated_at DESC,transaction_id`)
	} else {
		rows, err = r.db.Query(`SELECT DISTINCT t.* FROM transactions t LEFT JOIN transaction_access_grants g ON g.transaction_id=t.transaction_id WHERE t.owner_principal_id=? OR g.principal_id=? ORDER BY t.updated_at DESC,t.transaction_id`, principalID, principalID)
	}
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

func (r *Repository) ListTransactionGrants(transactionID string) ([]TransactionGrant, error) {
	rows, err := r.db.Query(`SELECT * FROM transaction_access_grants WHERE transaction_id=? ORDER BY principal_id`, transactionID)
	if err != nil {
		return nil, err
	}
	out := make([]TransactionGrant, 0, len(rows))
	for _, row := range rows {
		created, _ := parseTime(String(row, "created_at"))
		updated, _ := parseTime(String(row, "updated_at"))
		out = append(out, TransactionGrant{TransactionID: transactionID, PrincipalID: String(row, "principal_id"), Permission: TransactionAccess(String(row, "permission")), GrantedBy: String(row, "granted_by"), CreatedAt: created, UpdatedAt: updated})
	}
	return out, nil
}

func (r *Repository) GrantTransactionAccess(transactionID, actor, subject string, permission TransactionAccess, actorMayAdminAll bool, requestID string) error {
	if actor == "" || subject == "" || !validGrantPermission(permission) {
		return errors.New("valid actor, subject, and read/operate permission are required")
	}
	if actor == subject {
		return errors.New("owner/self grants are not permitted")
	}
	now := time.Now().UTC()
	return r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne(`SELECT owner_principal_id FROM transactions WHERE transaction_id=?`, transactionID)
		if err != nil {
			return err
		}
		owner := String(row, "owner_principal_id")
		if actor != owner && !actorMayAdminAll {
			return errors.New("only the transaction owner or all-scope operator may grant access")
		}
		if subject == owner {
			return errors.New("owner already has full access")
		}
		rows, err := tx.Query(`SELECT created_at FROM transaction_access_grants WHERE transaction_id=? AND principal_id=?`, transactionID, subject)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			_, err = tx.Exec(`INSERT INTO transaction_access_grants(transaction_id,principal_id,permission,granted_by,created_at,updated_at) VALUES(?,?,?,?,?,?)`, transactionID, subject, string(permission), actor, ts(now), ts(now))
		} else {
			_, err = tx.Exec(`UPDATE transaction_access_grants SET permission=?,granted_by=?,updated_at=? WHERE transaction_id=? AND principal_id=?`, string(permission), actor, ts(now), transactionID, subject)
		}
		if err != nil {
			return err
		}
		return appendTransactionAccessEvent(tx, transactionID, actor, subject, "granted", permission, requestID, now)
	})
}

func (r *Repository) RevokeTransactionAccess(transactionID, actor, subject string, actorMayAdminAll bool, requestID string) error {
	if actor == "" || subject == "" {
		return errors.New("actor and subject are required")
	}
	now := time.Now().UTC()
	return r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne(`SELECT owner_principal_id FROM transactions WHERE transaction_id=?`, transactionID)
		if err != nil {
			return err
		}
		owner := String(row, "owner_principal_id")
		if actor != owner && !actorMayAdminAll {
			return errors.New("only the transaction owner or all-scope operator may revoke access")
		}
		changes, err := tx.Exec(`DELETE FROM transaction_access_grants WHERE transaction_id=? AND principal_id=?`, transactionID, subject)
		if err != nil {
			return err
		}
		if changes != 1 {
			return ErrNotFound
		}
		return appendTransactionAccessEvent(tx, transactionID, actor, subject, "revoked", "", requestID, now)
	})
}

func accessEventFromRow(row Row) TransactionAccessEvent {
	created, _ := parseTime(String(row, "created_at"))
	return TransactionAccessEvent{Sequence: Int64(row, "sequence"), EventID: String(row, "event_id"), TransactionID: String(row, "transaction_id"), ActorPrincipalID: String(row, "actor_principal_id"), SubjectPrincipalID: String(row, "subject_principal_id"), Action: String(row, "action"), Permission: TransactionAccess(String(row, "permission")), RequestID: String(row, "request_id"), CreatedAt: created, PreviousDigest: String(row, "previous_digest"), EventDigest: String(row, "event_digest")}
}

func (r *Repository) VerifyTransactionAccessChain() (string, error) {
	rows, err := r.db.Query(`SELECT * FROM transaction_access_events ORDER BY sequence`)
	if err != nil {
		return "", err
	}
	previous := ""
	expected := int64(1)
	for _, row := range rows {
		e := accessEventFromRow(row)
		if e.Sequence != expected {
			return previous, fmt.Errorf("transaction access sequence gap: expected %d found %d", expected, e.Sequence)
		}
		if e.PreviousDigest != previous {
			return previous, fmt.Errorf("transaction access previous digest mismatch at sequence %d", e.Sequence)
		}
		m := accessHashMaterial{Sequence: e.Sequence, EventID: e.EventID, TransactionID: e.TransactionID, ActorPrincipalID: e.ActorPrincipalID, SubjectPrincipalID: e.SubjectPrincipalID, Action: e.Action, Permission: string(e.Permission), RequestID: e.RequestID, CreatedAt: ts(e.CreatedAt), PreviousDigest: e.PreviousDigest}
		digest := accessDigest(m)
		if digest != e.EventDigest {
			return previous, fmt.Errorf("transaction access event digest mismatch at sequence %d", e.Sequence)
		}
		previous = digest
		expected++
	}
	return previous, nil
}

func (r *Repository) TransactionAccessEvents(limit int) ([]TransactionAccessEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.Query(`SELECT * FROM transaction_access_events ORDER BY sequence DESC LIMIT ?`, int64(limit))
	if err != nil {
		return nil, err
	}
	out := make([]TransactionAccessEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, accessEventFromRow(row))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}
