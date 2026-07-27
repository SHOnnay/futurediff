package slo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"

type Policy struct {
	Version                   string `json:"version"`
	MaximumUnresolved         int64  `json:"maximum_unresolved"`
	MaximumUnknownEffects     int64  `json:"maximum_unknown_effects"`
	MaximumAuditErrors        int    `json:"maximum_audit_errors"`
	MaximumAuditWarnings      int    `json:"maximum_audit_warnings"`
	DaemonRequired            bool   `json:"daemon_required"`
	MaintenanceMustBeDisabled bool   `json:"maintenance_must_be_disabled"`
}

type Check struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Observed  any    `json:"observed,omitempty"`
	Threshold any    `json:"threshold,omitempty"`
	Message   string `json:"message"`
}
type Report struct {
	Version     string    `json:"version"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Policy      Policy    `json:"policy"`
	Status      string    `json:"status"`
	Checks      []Check   `json:"checks"`
	Digest      string    `json:"digest"`
}

func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	var x any
	if err := d.Decode(&x); err == nil {
		return p, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return p, err
	}
	return p, Validate(p)
}
func Validate(p Policy) error {
	if p.Version != Version {
		return fmt.Errorf("unsupported SLO policy version %q", p.Version)
	}
	if p.MaximumUnresolved < 0 || p.MaximumUnknownEffects < 0 || p.MaximumAuditErrors < 0 || p.MaximumAuditWarnings < 0 {
		return errors.New("SLO thresholds must be non-negative")
	}
	return nil
}

func Evaluate(repo *ledger.Repository, socket string, p Policy, now time.Time) (Report, error) {
	if err := Validate(p); err != nil {
		return Report{}, err
	}
	m, err := repo.Metrics()
	if err != nil {
		return Report{}, err
	}
	a, err := repo.Audit()
	if err != nil {
		return Report{}, err
	}
	r := Report{Version: Version, EvaluatedAt: now.UTC(), Policy: p, Status: "pass"}
	add := func(name string, ok bool, observed, threshold any, msg string) {
		s := "pass"
		if !ok {
			s = "fail"
			r.Status = "fail"
		}
		r.Checks = append(r.Checks, Check{Name: name, Status: s, Observed: observed, Threshold: threshold, Message: msg})
	}
	add("unresolved_transactions", m.UnresolvedTransactions <= p.MaximumUnresolved, m.UnresolvedTransactions, p.MaximumUnresolved, "unresolved transactions remain within limit")
	add("unknown_effects", m.UnknownEffects <= p.MaximumUnknownEffects, m.UnknownEffects, p.MaximumUnknownEffects, "unknown effects remain within limit")
	add("audit_errors", a.ErrorCount <= p.MaximumAuditErrors, a.ErrorCount, p.MaximumAuditErrors, "audit errors remain within limit")
	add("audit_warnings", a.WarningCount <= p.MaximumAuditWarnings, a.WarningCount, p.MaximumAuditWarnings, "audit warnings remain within limit")
	if p.DaemonRequired || p.MaintenanceMustBeDisabled {
		raw, e := api.NewClient(socket).Do("GET", "/v1/health", nil)
		if e != nil {
			add("daemon_health", !p.DaemonRequired, e.Error(), "reachable", "daemon must be reachable")
		} else {
			var h map[string]any
			if e := json.Unmarshal(raw, &h); e != nil {
				return Report{}, e
			}
			add("daemon_health", true, "ok", "ok", "daemon health endpoint responded")
			if p.MaintenanceMustBeDisabled {
				enabled := false
				if x, ok := h["maintenance"].(map[string]any); ok {
					enabled, _ = x["enabled"].(bool)
				}
				add("maintenance_disabled", !enabled, enabled, false, "maintenance mode must be disabled")
			}
		}
	}
	r.Digest = reportDigest(r)
	return r, nil
}
func reportDigest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
