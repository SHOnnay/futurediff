package retentionpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/retention"
)

const Version = "0.1"

type Policy struct {
	Version            string `json:"version"`
	TerminalAfterHours int64  `json:"terminal_after_hours"`
	ApplyEnabled       bool   `json:"apply_enabled"`
	MaximumCandidates  int    `json:"maximum_candidates,omitempty"`
	MaximumBytes       int64  `json:"maximum_bytes,omitempty"`
}

type Evaluation struct {
	Version      string         `json:"version"`
	Policy       Policy         `json:"policy"`
	PolicyDigest string         `json:"policy_digest"`
	EvaluatedAt  time.Time      `json:"evaluated_at"`
	Plan         retention.Plan `json:"plan"`
	WithinLimits bool           `json:"within_limits"`
	Findings     []string       `json:"findings,omitempty"`
}

func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported retention policy version %q", p.Version)
	}
	if p.TerminalAfterHours < 0 {
		return errors.New("terminal_after_hours must be non-negative")
	}
	if p.MaximumCandidates < 0 || p.MaximumBytes < 0 {
		return errors.New("retention limits must be non-negative")
	}
	return nil
}

func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return p, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return p, err
	}
	return p, Validate(p)
}

func Evaluate(repo *ledger.Repository, root string, p Policy, now time.Time) (Evaluation, error) {
	if err := Validate(p); err != nil {
		return Evaluation{}, err
	}
	plan, err := retention.BuildPlan(repo, root, now.UTC().Add(-time.Duration(p.TerminalAfterHours)*time.Hour))
	if err != nil {
		return Evaluation{}, err
	}
	d, _ := domain.Digest(p)
	e := Evaluation{Version: Version, Policy: p, PolicyDigest: d, EvaluatedAt: now.UTC(), Plan: plan, WithinLimits: true}
	if p.MaximumCandidates > 0 && len(plan.Candidates) > p.MaximumCandidates {
		e.WithinLimits = false
		e.Findings = append(e.Findings, fmt.Sprintf("candidate count %d exceeds limit %d", len(plan.Candidates), p.MaximumCandidates))
	}
	if p.MaximumBytes > 0 && plan.TotalBytes > p.MaximumBytes {
		e.WithinLimits = false
		e.Findings = append(e.Findings, fmt.Sprintf("candidate bytes %d exceeds limit %d", plan.TotalBytes, p.MaximumBytes))
	}
	return e, nil
}

func Apply(repo *ledger.Repository, e Evaluation, confirmation string) (retention.Result, error) {
	if !e.Policy.ApplyEnabled {
		return retention.Result{}, errors.New("retention policy does not allow apply")
	}
	if !e.WithinLimits {
		return retention.Result{}, errors.New("retention plan exceeds policy limits")
	}
	return retention.Apply(repo, e.Plan, confirmation)
}
