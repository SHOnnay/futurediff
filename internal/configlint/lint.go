package configlint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/internal/agentbench"
	"github.com/SHOnnay/futurediff/internal/backupcatalog"
	"github.com/SHOnnay/futurediff/internal/configattest"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/idempotencygc"
	"github.com/SHOnnay/futurediff/internal/installer"
	"github.com/SHOnnay/futurediff/internal/ratelimit"
	"github.com/SHOnnay/futurediff/internal/repoadmission"
	"github.com/SHOnnay/futurediff/internal/storageguard"
	"github.com/SHOnnay/futurediff/internal/transactionexpiry"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type Finding struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Report struct {
	Path     string    `json:"path"`
	Kind     string    `json:"kind"`
	Valid    bool      `json:"valid"`
	Findings []Finding `json:"findings,omitempty"`
}

func Lint(path, kind string) Report {
	report := Report{Path: path, Kind: kind, Valid: true}
	fail := func(code string, err error) {
		report.Valid = false
		report.Findings = append(report.Findings, Finding{Level: "error", Code: code, Message: err.Error()})
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fail("read_failed", err)
		return report
	}
	if kind == "auto" || kind == "" {
		kind = detect(data)
		report.Kind = kind
	}
	switch kind {
	case "credentials":
		if _, err := credentials.LoadConfig(path); err != nil {
			fail("invalid_credentials", err)
		}
	case "verification":
		if _, err := verification.Parse(data); err != nil {
			fail("invalid_verification_contract", err)
		}
	case "agent-run":
		var run agentbench.Run
		if err := strictJSON(data, &run); err != nil {
			fail("invalid_agent_run_json", err)
		} else if err := run.Validate(); err != nil {
			fail("invalid_agent_run", err)
		}
	case "installer-plan":
		var plan installer.Plan
		if err := strictJSON(data, &plan); err != nil {
			fail("invalid_installer_plan_json", err)
		} else if err := validatePlan(plan); err != nil {
			fail("invalid_installer_plan", err)
		}
	case "opencode":
		if err := validateOpenCode(data); err != nil {
			fail("invalid_opencode_profile", err)
		}
	case "rate-policy":
		if _, err := ratelimit.Load(path); err != nil {
			fail("invalid_rate_policy", err)
		}
	case "repository-policy":
		if _, err := repoadmission.Load(path); err != nil {
			fail("invalid_repository_policy", err)
		}
	case "storage-policy":
		if _, err := storageguard.Load(path); err != nil {
			fail("invalid_storage_policy", err)
		}
	case "transaction-expiry-policy":
		if _, err := transactionexpiry.Load(path); err != nil {
			fail("invalid_transaction_expiry_policy", err)
		}
	case "idempotency-retention-policy":
		if _, err := idempotencygc.Load(path); err != nil {
			fail("invalid_idempotency_retention_policy", err)
		}
	case "backup-retention-policy":
		if _, err := backupcatalog.Load(path); err != nil {
			fail("invalid_backup_retention_policy", err)
		}
	case "config-attestation":
		if _, err := configattest.Load(path); err != nil {
			fail("invalid_config_attestation", err)
		}
	case "json":
		var value any
		if err := strictJSON(data, &value); err != nil {
			fail("invalid_json", err)
		}
	default:
		fail("unsupported_kind", fmt.Errorf("unsupported config kind %q", kind))
	}
	sort.Slice(report.Findings, func(i, j int) bool { return report.Findings[i].Code < report.Findings[j].Code })
	return report
}
func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := d.Decode(&trailing); err == nil {
		return errors.New("multiple JSON values are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
func detect(data []byte) string {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return "json"
	}
	if _, ok := m["credentials"]; ok {
		return "credentials"
	}
	if _, ok := m["checks"]; ok {
		return "verification"
	}
	if _, ok := m["run_id"]; ok {
		return "agent-run"
	}
	if _, ok := m["actions"]; ok {
		return "installer-plan"
	}
	if _, ok := m["mcp"]; ok {
		return "opencode"
	}
	if _, ok := m["read_requests_per_minute"]; ok {
		return "rate-policy"
	}
	if _, ok := m["minimum_free_bytes"]; ok {
		return "storage-policy"
	}
	if _, ok := m["state_after_hours"]; ok {
		return "transaction-expiry-policy"
	}
	if _, ok := m["completed_after_hours"]; ok {
		return "idempotency-retention-policy"
	}
	if _, ok := m["backup_root"]; ok {
		return "backup-retention-policy"
	}
	if _, ok := m["file_sha256"]; ok {
		if _, ok := m["signature"]; ok {
			return "config-attestation"
		}
	}
	return "json"
}
func validatePlan(plan installer.Plan) error {
	if len(plan.Actions) == 0 {
		return errors.New("installer plan has no actions")
	}
	seen := map[string]bool{}
	for _, a := range plan.Actions {
		if !filepath.IsAbs(a.Target) {
			return fmt.Errorf("target must be absolute: %s", a.Target)
		}
		if seen[a.Target] {
			return fmt.Errorf("duplicate target: %s", a.Target)
		}
		seen[a.Target] = true
		switch a.Kind {
		case "binary", "systemd-user-service", "launchd-user-service":
		default:
			return fmt.Errorf("unsupported action kind %q", a.Kind)
		}
		if a.Kind == "binary" && !filepath.IsAbs(a.Source) {
			return fmt.Errorf("binary source must be absolute: %s", a.Source)
		}
	}
	return nil
}
func validateOpenCode(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	mcp, ok := m["mcp"].(map[string]any)
	if !ok {
		return errors.New("mcp object is required")
	}
	fd, ok := mcp["futurediff"].(map[string]any)
	if !ok {
		return errors.New("mcp.futurediff object is required")
	}
	if fmt.Sprint(fd["type"]) != "local" {
		return errors.New("FutureDiff MCP server must be local")
	}
	cmd, ok := fd["command"].([]any)
	if !ok || len(cmd) < 3 {
		return errors.New("FutureDiff MCP command array is incomplete")
	}
	joined := ""
	for _, v := range cmd {
		joined += " " + fmt.Sprint(v)
	}
	if !strings.Contains(joined, "futurediff-mcp") || !strings.Contains(joined, "--socket") {
		return errors.New("command must launch futurediff-mcp with --socket")
	}
	lower := strings.ToLower(string(data))
	for _, forbidden := range []string{"transaction_approve", "transaction_commit", "credential"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("profile exposes forbidden authority %s", forbidden)
		}
	}
	return nil
}
