package readiness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/operatorreceipt"
	"github.com/SHOnnay/futurediff/internal/slo"
)

const Version = "0.1"

type Manifest struct {
	Version                    string `json:"version"`
	RequireAuditHealthy        bool   `json:"require_audit_healthy"`
	SLOPolicy                  string `json:"slo_policy,omitempty"`
	RequiredAPIContractDigest  string `json:"required_api_contract_digest,omitempty"`
	RequireMaintenanceDisabled bool   `json:"require_maintenance_disabled"`
	OperatorReceiptKeyring     string `json:"operator_receipt_keyring,omitempty"`
	RequireOperatorReceipts    bool   `json:"require_operator_receipts"`
}

type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Evidence any    `json:"evidence,omitempty"`
}
type Report struct {
	Version     string    `json:"version"`
	EvaluatedAt time.Time `json:"evaluated_at"`
	Ready       bool      `json:"ready"`
	Checks      []Check   `json:"checks"`
	Digest      string    `json:"digest"`
}

func Load(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	var x any
	if err := d.Decode(&x); err == nil {
		return m, errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return m, err
	}
	if m.Version != Version {
		return m, fmt.Errorf("unsupported readiness manifest version %q", m.Version)
	}
	base := filepath.Dir(path)
	for _, p := range []*string{&m.SLOPolicy, &m.OperatorReceiptKeyring} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	return m, nil
}

func Evaluate(repo *ledger.Repository, root, socket string, m Manifest, now time.Time) (Report, error) {
	r := Report{Version: Version, EvaluatedAt: now.UTC(), Ready: true}
	add := func(name string, ok bool, msg string, e any) {
		s := "pass"
		if !ok {
			s = "fail"
			r.Ready = false
		}
		r.Checks = append(r.Checks, Check{Name: name, Status: s, Message: msg, Evidence: e})
	}
	if m.RequireAuditHealthy {
		a, err := repo.Audit()
		if err != nil {
			return r, err
		}
		add("ledger_audit", a.Healthy, "ledger audit must be healthy", map[string]any{"errors": a.ErrorCount, "warnings": a.WarningCount})
	}
	if m.SLOPolicy != "" {
		p, err := slo.Load(m.SLOPolicy)
		if err != nil {
			return r, err
		}
		s, err := slo.Evaluate(repo, socket, p, now)
		if err != nil {
			return r, err
		}
		add("slo", s.Status == "pass", "service-level objectives must pass", s)
	}
	if m.RequiredAPIContractDigest != "" {
		current := apicontract.Current()
		add("api_contract", current.Digest == m.RequiredAPIContractDigest, "local API contract digest must match", map[string]string{"expected": m.RequiredAPIContractDigest, "observed": current.Digest})
	}
	if m.RequireMaintenanceDisabled {
		st, err := (&maintenance.Manager{Path: filepath.Join(root, "maintenance.json")}).Status(now)
		if err != nil {
			return r, err
		}
		add("maintenance", !st.Enabled, "maintenance mode must be disabled", st)
	}
	if m.RequireOperatorReceipts {
		if m.OperatorReceiptKeyring == "" {
			return r, errors.New("operator receipt keyring is required")
		}
		ring, err := operatorapproval.LoadKeyring(m.OperatorReceiptKeyring)
		if err != nil {
			return r, err
		}
		v, err := operatorreceipt.Verify(filepath.Join(root, "operator-receipts"), ring, now)
		if err != nil {
			return r, err
		}
		add("operator_receipts", v.Valid && v.Count > 0, "operator receipt chain must be valid and non-empty", v)
	}
	r.Digest = digest(r)
	return r, nil
}
func digest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
