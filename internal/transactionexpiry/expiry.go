package transactionexpiry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"
const Confirmation = "EXPIRE_STALE_FUTUREDIFF_TRANSACTIONS"

var safeStates = map[domain.TransactionState]bool{
	domain.StateActive: true, domain.StateSealed: true, domain.StateFailedVerification: true,
	domain.StateReady: true, domain.StateStale: true,
}

type Policy struct {
	Version           string           `json:"version"`
	ApplyEnabled      bool             `json:"apply_enabled"`
	MaximumCandidates int              `json:"maximum_candidates,omitempty"`
	StateAfterHours   map[string]int64 `json:"state_after_hours"`
}
type Candidate struct {
	TransactionID string                  `json:"transaction_id"`
	Status        domain.TransactionState `json:"status"`
	UpdatedAt     time.Time               `json:"updated_at"`
	ExpiresBefore time.Time               `json:"expires_before"`
	AgeHours      int64                   `json:"age_hours"`
}
type Plan struct {
	Version      string      `json:"version"`
	PolicyDigest string      `json:"policy_digest"`
	PlannedAt    time.Time   `json:"planned_at"`
	Candidates   []Candidate `json:"candidates"`
	WithinLimits bool        `json:"within_limits"`
	PlanDigest   string      `json:"plan_digest"`
	Policy       Policy      `json:"policy"`
}
type Result struct {
	AppliedAt      time.Time `json:"applied_at"`
	Expired        int       `json:"expired"`
	TransactionIDs []string  `json:"transaction_ids"`
	PlanDigest     string    `json:"plan_digest"`
}

func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported transaction-expiry policy version %q", p.Version)
	}
	if len(p.StateAfterHours) == 0 {
		return errors.New("state_after_hours is required")
	}
	if p.MaximumCandidates < 0 {
		return errors.New("maximum_candidates must be non-negative")
	}
	for raw, h := range p.StateAfterHours {
		st := domain.TransactionState(raw)
		if !safeStates[st] {
			return fmt.Errorf("state %q is not safe for automatic expiry", raw)
		}
		if h <= 0 {
			return fmt.Errorf("expiry hours for %s must be positive", raw)
		}
	}
	return nil
}
func Load(path string) (Policy, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Policy{}, e
	}
	var p Policy
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&p); e != nil {
		return p, e
	}
	var extra any
	if e = d.Decode(&extra); e == nil {
		return p, errors.New("trailing JSON data")
	} else if !errors.Is(e, io.EOF) {
		return p, e
	}
	return p, Validate(p)
}
func BuildPlan(repo *ledger.Repository, p Policy, now time.Time) (Plan, error) {
	if e := Validate(p); e != nil {
		return Plan{}, e
	}
	now = now.UTC()
	pd, _ := domain.Digest(p)
	plan := Plan{Version: Version, PolicyDigest: pd, PlannedAt: now, WithinLimits: true, Policy: p}
	states := make([]string, 0, len(p.StateAfterHours))
	for s := range p.StateAfterHours {
		states = append(states, s)
	}
	sort.Strings(states)
	for _, raw := range states {
		cutoff := now.Add(-time.Duration(p.StateAfterHours[raw]) * time.Hour)
		txs, e := repo.TransactionsByStatusBefore(domain.TransactionState(raw), cutoff)
		if e != nil {
			return Plan{}, e
		}
		for _, tx := range txs {
			plan.Candidates = append(plan.Candidates, Candidate{TransactionID: tx.ID, Status: tx.Status, UpdatedAt: tx.UpdatedAt, ExpiresBefore: cutoff, AgeHours: int64(now.Sub(tx.UpdatedAt).Hours())})
		}
	}
	sort.Slice(plan.Candidates, func(i, j int) bool {
		if plan.Candidates[i].UpdatedAt.Equal(plan.Candidates[j].UpdatedAt) {
			return plan.Candidates[i].TransactionID < plan.Candidates[j].TransactionID
		}
		return plan.Candidates[i].UpdatedAt.Before(plan.Candidates[j].UpdatedAt)
	})
	if p.MaximumCandidates > 0 && len(plan.Candidates) > p.MaximumCandidates {
		plan.WithinLimits = false
	}
	material := struct {
		Version      string      `json:"version"`
		PolicyDigest string      `json:"policy_digest"`
		PlannedAt    string      `json:"planned_at"`
		Candidates   []Candidate `json:"candidates"`
	}{Version, pd, now.Format(time.RFC3339Nano), plan.Candidates}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	plan.PlanDigest = hex.EncodeToString(sum[:])
	return plan, nil
}
func Apply(svc *app.Service, repo *ledger.Repository, plan Plan, confirmation string, now time.Time) (Result, error) {
	if !plan.Policy.ApplyEnabled {
		return Result{}, errors.New("transaction-expiry policy does not allow apply")
	}
	if !plan.WithinLimits {
		return Result{}, errors.New("transaction-expiry plan exceeds maximum_candidates")
	}
	if confirmation != Confirmation {
		return Result{}, errors.New("exact expiry confirmation is required")
	}
	result := Result{AppliedAt: now.UTC(), PlanDigest: plan.PlanDigest}
	for _, c := range plan.Candidates {
		current, e := repo.Get(c.TransactionID)
		if e != nil {
			return result, e
		}
		if current.Status != c.Status || !current.UpdatedAt.Equal(c.UpdatedAt) {
			return result, fmt.Errorf("transaction %s changed after planning", c.TransactionID)
		}
		if _, e = svc.AbortWithReason(c.TransactionID, "expiry", "expired by policy "+plan.PolicyDigest); e != nil {
			return result, e
		}
		if e = repo.RecordExpiryAction(ledger.ExpiryAction{TransactionID: c.TransactionID, PriorStatus: c.Status, PolicyDigest: plan.PolicyDigest, AppliedAt: now.UTC()}); e != nil {
			return result, e
		}
		result.Expired++
		result.TransactionIDs = append(result.TransactionIDs, c.TransactionID)
	}
	return result, nil
}
