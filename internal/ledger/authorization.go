package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type AuthorizationDecisionInput struct {
	PrincipalID      string
	OperationID      string
	ResourceID       string
	Allowed          bool
	Source           string
	ReasonCode       string
	PolicyDigest     string
	Roles            []string
	CapabilityDigest string
	RequestID        string
}

type authorizationHashMaterial struct {
	Sequence         int64  `json:"sequence"`
	DecisionID       string `json:"decision_id"`
	PrincipalID      string `json:"principal_id"`
	OperationID      string `json:"operation_id"`
	ResourceID       string `json:"resource_id,omitempty"`
	Allowed          bool   `json:"allowed"`
	Source           string `json:"source"`
	ReasonCode       string `json:"reason_code"`
	PolicyDigest     string `json:"policy_digest,omitempty"`
	RoleNames        string `json:"role_names,omitempty"`
	CapabilityDigest string `json:"capability_digest,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	CreatedAt        string `json:"created_at"`
	PreviousDigest   string `json:"previous_digest,omitempty"`
}

type AuthorizationDecisionEvent struct {
	Sequence         int64     `json:"sequence"`
	DecisionID       string    `json:"decision_id"`
	PrincipalID      string    `json:"principal_id"`
	OperationID      string    `json:"operation_id"`
	ResourceID       string    `json:"resource_id,omitempty"`
	Allowed          bool      `json:"allowed"`
	Source           string    `json:"source"`
	ReasonCode       string    `json:"reason_code"`
	PolicyDigest     string    `json:"policy_digest,omitempty"`
	Roles            []string  `json:"roles,omitempty"`
	CapabilityDigest string    `json:"capability_digest,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	PreviousDigest   string    `json:"previous_digest,omitempty"`
	EventDigest      string    `json:"event_digest"`
}

type AuthorizationSummary struct {
	Total      int64                        `json:"total"`
	Allowed    int64                        `json:"allowed"`
	Denied     int64                        `json:"denied"`
	Recent     []AuthorizationDecisionEvent `json:"recent"`
	ChainValid bool                         `json:"chain_valid"`
	ChainError string                       `json:"chain_error,omitempty"`
	HeadDigest string                       `json:"head_digest,omitempty"`
}

func authorizationDigest(m authorizationHashMaterial) string {
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func normalizeRoles(roles []string) string {
	if len(roles) == 0 {
		return ""
	}
	cp := append([]string(nil), roles...)
	sort.Strings(cp)
	return strings.Join(cp, ",")
}
func splitRoles(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func (r *Repository) RecordAuthorizationDecision(input AuthorizationDecisionInput) error {
	if input.PrincipalID == "" || input.OperationID == "" || input.Source == "" || input.ReasonCode == "" {
		return errors.New("authorization decision identity fields are required")
	}
	now := time.Now().UTC()
	id := domain.NewID("authz")
	roles := normalizeRoles(input.Roles)
	return r.db.WithTx(func(tx *Tx) error {
		previous := ""
		rows, err := tx.Query(`SELECT event_digest FROM authorization_decisions ORDER BY sequence DESC LIMIT 1`)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			previous = String(rows[0], "event_digest")
		}
		seqRow, err := tx.QueryOne(`SELECT COALESCE(MAX(sequence),0)+1 AS next_sequence FROM authorization_decisions`)
		if err != nil {
			return err
		}
		seq := Int64(seqRow, "next_sequence")
		m := authorizationHashMaterial{Sequence: seq, DecisionID: id, PrincipalID: input.PrincipalID, OperationID: input.OperationID, ResourceID: input.ResourceID, Allowed: input.Allowed, Source: input.Source, ReasonCode: input.ReasonCode, PolicyDigest: input.PolicyDigest, RoleNames: roles, CapabilityDigest: input.CapabilityDigest, RequestID: input.RequestID, CreatedAt: ts(now), PreviousDigest: previous}
		digest := authorizationDigest(m)
		allowed := int64(0)
		if input.Allowed {
			allowed = 1
		}
		_, err = tx.Exec(`INSERT INTO authorization_decisions(sequence,decision_id,principal_id,operation_id,resource_id,allowed,source,reason_code,policy_digest,role_names,capability_digest,request_id,created_at,previous_digest,event_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, seq, id, input.PrincipalID, input.OperationID, nullString(input.ResourceID), allowed, input.Source, input.ReasonCode, nullString(input.PolicyDigest), nullString(roles), nullString(input.CapabilityDigest), nullString(input.RequestID), ts(now), nullString(previous), digest)
		return err
	})
}

func authzEventFromRow(row Row) AuthorizationDecisionEvent {
	created, _ := parseTime(String(row, "created_at"))
	return AuthorizationDecisionEvent{Sequence: Int64(row, "sequence"), DecisionID: String(row, "decision_id"), PrincipalID: String(row, "principal_id"), OperationID: String(row, "operation_id"), ResourceID: String(row, "resource_id"), Allowed: Int64(row, "allowed") != 0, Source: String(row, "source"), ReasonCode: String(row, "reason_code"), PolicyDigest: String(row, "policy_digest"), Roles: splitRoles(String(row, "role_names")), CapabilityDigest: String(row, "capability_digest"), RequestID: String(row, "request_id"), CreatedAt: created, PreviousDigest: String(row, "previous_digest"), EventDigest: String(row, "event_digest")}
}
func (r *Repository) VerifyAuthorizationDecisionChain() (string, error) {
	rows, err := r.db.Query(`SELECT * FROM authorization_decisions ORDER BY sequence`)
	if err != nil {
		return "", err
	}
	previous := ""
	expected := int64(1)
	for _, row := range rows {
		e := authzEventFromRow(row)
		if e.Sequence != expected {
			return previous, fmt.Errorf("authorization sequence gap: expected %d found %d", expected, e.Sequence)
		}
		if e.PreviousDigest != previous {
			return previous, fmt.Errorf("authorization previous digest mismatch at sequence %d", e.Sequence)
		}
		m := authorizationHashMaterial{Sequence: e.Sequence, DecisionID: e.DecisionID, PrincipalID: e.PrincipalID, OperationID: e.OperationID, ResourceID: e.ResourceID, Allowed: e.Allowed, Source: e.Source, ReasonCode: e.ReasonCode, PolicyDigest: e.PolicyDigest, RoleNames: normalizeRoles(e.Roles), CapabilityDigest: e.CapabilityDigest, RequestID: e.RequestID, CreatedAt: ts(e.CreatedAt), PreviousDigest: e.PreviousDigest}
		d := authorizationDigest(m)
		if e.EventDigest != d {
			return previous, fmt.Errorf("authorization event digest mismatch at sequence %d", e.Sequence)
		}
		previous = d
		expected++
	}
	return previous, nil
}
func (r *Repository) AuthorizationSummary(limit int) (AuthorizationSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.Query(`SELECT * FROM authorization_decisions ORDER BY sequence DESC LIMIT ?`, int64(limit))
	if err != nil {
		return AuthorizationSummary{}, err
	}
	s := AuthorizationSummary{Recent: make([]AuthorizationDecisionEvent, 0, len(rows))}
	count, err := r.db.QueryOne(`SELECT COUNT(*) AS total,SUM(CASE WHEN allowed=1 THEN 1 ELSE 0 END) AS allowed,SUM(CASE WHEN allowed=0 THEN 1 ELSE 0 END) AS denied FROM authorization_decisions`)
	if err != nil {
		return s, err
	}
	s.Total = Int64(count, "total")
	s.Allowed = Int64(count, "allowed")
	s.Denied = Int64(count, "denied")
	for _, row := range rows {
		s.Recent = append(s.Recent, authzEventFromRow(row))
	}
	head, verr := r.VerifyAuthorizationDecisionChain()
	s.HeadDigest = head
	s.ChainValid = verr == nil
	if verr != nil {
		s.ChainError = verr.Error()
	}
	return s, nil
}

func (r *Repository) ConsumeAuthorizationCapability(capabilityID, principalID, operationID, resourceID, capabilityDigest string) error {
	if capabilityID == "" || principalID == "" || operationID == "" || capabilityDigest == "" {
		return errors.New("capability use fields are required")
	}
	return r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query(`SELECT capability_id FROM authorization_capability_uses WHERE capability_id=?`, capabilityID)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			return errors.New("capability was already used")
		}
		_, err = tx.Exec(`INSERT INTO authorization_capability_uses(capability_id,principal_id,operation_id,resource_id,capability_digest,used_at) VALUES(?,?,?,?,?,?)`, capabilityID, principalID, operationID, nullString(resourceID), capabilityDigest, ts(time.Now().UTC()))
		return err
	})
}
