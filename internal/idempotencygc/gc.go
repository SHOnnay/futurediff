package idempotencygc

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

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"
const Confirmation = "DELETE_EXPIRED_FUTUREDIFF_IDEMPOTENCY_RECORDS"

type Policy struct {
	Version              string `json:"version"`
	ApplyEnabled         bool   `json:"apply_enabled"`
	CompletedAfterHours  int64  `json:"completed_after_hours"`
	InProgressAfterHours int64  `json:"in_progress_after_hours"`
	MaximumCandidates    int    `json:"maximum_candidates,omitempty"`
}
type Candidate struct {
	PrincipalDigest string    `json:"principal_digest"`
	KeyDigest       string    `json:"key_digest"`
	RequestDigest   string    `json:"request_digest"`
	State           string    `json:"state"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Plan struct {
	Version          string      `json:"version"`
	Policy           Policy      `json:"policy"`
	PolicyDigest     string      `json:"policy_digest"`
	PlannedAt        time.Time   `json:"planned_at"`
	CompletedBefore  time.Time   `json:"completed_before"`
	InProgressBefore time.Time   `json:"in_progress_before"`
	Candidates       []Candidate `json:"candidates"`
	WithinLimits     bool        `json:"within_limits"`
	PlanDigest       string      `json:"plan_digest"`
	records          []ledger.APIRequestRecord
}
type Result struct {
	AppliedAt         time.Time `json:"applied_at"`
	CompletedDeleted  int64     `json:"completed_deleted"`
	InProgressDeleted int64     `json:"in_progress_deleted"`
	PlanDigest        string    `json:"plan_digest"`
}

func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported idempotency retention policy version %q", p.Version)
	}
	if p.CompletedAfterHours <= 0 || p.InProgressAfterHours <= 0 {
		return errors.New("retention hours must be positive")
	}
	if p.MaximumCandidates < 0 {
		return errors.New("maximum_candidates must be non-negative")
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
func digestText(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func BuildPlan(repo *ledger.Repository, p Policy, now time.Time) (Plan, error) {
	if e := Validate(p); e != nil {
		return Plan{}, e
	}
	now = now.UTC()
	completedBefore := now.Add(-time.Duration(p.CompletedAfterHours) * time.Hour)
	inProgressBefore := now.Add(-time.Duration(p.InProgressAfterHours) * time.Hour)
	completed, e := repo.IdempotencyBefore("completed", completedBefore)
	if e != nil {
		return Plan{}, e
	}
	progress, e := repo.IdempotencyBefore("in_progress", inProgressBefore)
	if e != nil {
		return Plan{}, e
	}
	records := append(completed, progress...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			if records[i].PrincipalID == records[j].PrincipalID {
				return records[i].IdempotencyKey < records[j].IdempotencyKey
			}
			return records[i].PrincipalID < records[j].PrincipalID
		}
		return records[i].UpdatedAt.Before(records[j].UpdatedAt)
	})
	pd, _ := domain.Digest(p)
	plan := Plan{Version: Version, Policy: p, PolicyDigest: pd, PlannedAt: now, CompletedBefore: completedBefore, InProgressBefore: inProgressBefore, WithinLimits: true, records: records}
	for _, r := range records {
		plan.Candidates = append(plan.Candidates, Candidate{PrincipalDigest: digestText(r.PrincipalID), KeyDigest: digestText(r.IdempotencyKey), RequestDigest: r.RequestDigest, State: r.State, UpdatedAt: r.UpdatedAt})
	}
	if p.MaximumCandidates > 0 && len(records) > p.MaximumCandidates {
		plan.WithinLimits = false
	}
	material := struct {
		Version          string      `json:"version"`
		PolicyDigest     string      `json:"policy_digest"`
		CompletedBefore  string      `json:"completed_before"`
		InProgressBefore string      `json:"in_progress_before"`
		Candidates       []Candidate `json:"candidates"`
	}{Version, pd, completedBefore.Format(time.RFC3339Nano), inProgressBefore.Format(time.RFC3339Nano), plan.Candidates}
	data, _ := json.Marshal(material)
	sum := sha256.Sum256(data)
	plan.PlanDigest = hex.EncodeToString(sum[:])
	return plan, nil
}
func Apply(repo *ledger.Repository, plan Plan, confirmation string, now time.Time) (Result, error) {
	if !plan.Policy.ApplyEnabled {
		return Result{}, errors.New("idempotency retention policy does not allow apply")
	}
	if !plan.WithinLimits {
		return Result{}, errors.New("idempotency GC plan exceeds maximum_candidates")
	}
	if confirmation != Confirmation {
		return Result{}, errors.New("exact idempotency GC confirmation is required")
	}
	c, p, e := repo.DeleteIdempotencyRecords(plan.records)
	if e != nil {
		return Result{}, e
	}
	if e = repo.RecordIdempotencyGC("", c, p, plan.CompletedBefore, plan.InProgressBefore, plan.PlanDigest, now.UTC()); e != nil {
		return Result{}, e
	}
	return Result{AppliedAt: now.UTC(), CompletedDeleted: c, InProgressDeleted: p, PlanDigest: plan.PlanDigest}, nil
}
