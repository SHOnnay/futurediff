package verification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type Contract struct {
	FormatVersion string  `json:"format_version"`
	ContractID    string  `json:"contract_id"`
	PolicyVersion string  `json:"policy_version"`
	Checks        []Check `json:"checks"`
}

type Check struct {
	CheckID        string   `json:"check_id"`
	Required       bool     `json:"required"`
	DependsOn      []string `json:"depends_on,omitempty"`
	Executor       string   `json:"executor"`
	Type           string   `json:"type"`
	Path           string   `json:"path,omitempty"`
	Digest         string   `json:"digest,omitempty"`
	Command        []string `json:"command,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

func Parse(b []byte) (Contract, error) {
	var c Contract
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, Validate(c)
}
func Validate(c Contract) error {
	if c.FormatVersion != "0.1" {
		return errors.New("only verification contract 0.1 is supported")
	}
	if c.ContractID == "" || c.PolicyVersion == "" {
		return errors.New("contract_id and policy_version are required")
	}
	if len(c.Checks) == 0 {
		return errors.New("at least one check is required")
	}
	ids := map[string]bool{}
	required := false
	for _, ch := range c.Checks {
		if ch.CheckID == "" {
			return errors.New("check_id is required")
		}
		if ids[ch.CheckID] {
			return fmt.Errorf("duplicate check %s", ch.CheckID)
		}
		ids[ch.CheckID] = true
		if ch.Required {
			required = true
		}
		switch ch.Executor {
		case "workspace_assertion":
			if ch.Type != "file_exists" && ch.Type != "file_absent" && ch.Type != "file_sha256" {
				return fmt.Errorf("unsupported assertion %s", ch.Type)
			}
		case "local_command", "oci_command":
			if len(ch.Command) == 0 {
				return fmt.Errorf("command required for %s", ch.CheckID)
			}
		default:
			return fmt.Errorf("unsupported executor %s", ch.Executor)
		}
	}
	if !required {
		return errors.New("at least one required check is required")
	}
	for _, ch := range c.Checks {
		for _, d := range ch.DependsOn {
			if !ids[d] {
				return fmt.Errorf("unknown dependency %s", d)
			}
		}
	}
	_, err := Topological(c.Checks)
	return err
}

func Topological(checks []Check) ([]Check, error) {
	byID := map[string]Check{}
	pending := map[string]map[string]bool{}
	for _, ch := range checks {
		byID[ch.CheckID] = ch
		deps := map[string]bool{}
		for _, d := range ch.DependsOn {
			deps[d] = true
		}
		pending[ch.CheckID] = deps
	}
	var order []Check
	for len(pending) > 0 {
		var ready []string
		for id, deps := range pending {
			if len(deps) == 0 {
				ready = append(ready, id)
			}
		}
		sort.Strings(ready)
		if len(ready) == 0 {
			return nil, errors.New("verification graph contains a cycle")
		}
		for _, id := range ready {
			order = append(order, byID[id])
			delete(pending, id)
			for _, deps := range pending {
				delete(deps, id)
			}
		}
	}
	return order, nil
}

type Engine struct {
	AllowLocalCommands bool
	OCI                *runtimeoci.Runner
}

func (e Engine) Run(transactionID string, workspace domain.Workspace, patch domain.Patch, contract Contract) (domain.VerificationReport, error) {
	return e.RunWithMaterial(transactionID, workspace, patch, contract, patch.ApprovalMaterialDigest)
}

func (e Engine) RunWithMaterial(transactionID string, workspace domain.Workspace, patch domain.Patch, contract Contract, materialDigest string) (domain.VerificationReport, error) {
	if strings.TrimSpace(materialDigest) == "" {
		return domain.VerificationReport{}, errors.New("verification material digest is required")
	}
	if err := Validate(contract); err != nil {
		return domain.VerificationReport{}, err
	}
	order, _ := Topological(contract.Checks)
	resultByID := map[string]domain.VerificationCheckResult{}
	var results []domain.VerificationCheckResult
	for _, ch := range order {
		blocked := false
		for _, d := range ch.DependsOn {
			if resultByID[d].Status != "pass" {
				blocked = true
			}
		}
		status, message := "blocked", "one or more dependencies did not pass"
		runtimeEvidenceDigest := ""
		if !blocked {
			var err error
			status, message, runtimeEvidenceDigest, err = e.execute(transactionID, workspace, ch)
			if err != nil {
				return domain.VerificationReport{}, err
			}
		}
		specDigest, _ := domain.Digest(ch)
		evidenceDigest := runtimeEvidenceDigest
		if evidenceDigest == "" {
			evidenceDigest, _ = domain.Digest(map[string]any{"check_id": ch.CheckID, "status": status, "message": message, "patch_sha256": patch.PatchSHA256})
		}
		cacheKey, _ := domain.Digest(map[string]any{"check_spec": specDigest, "patch": patch.PatchSHA256, "executor": ch.Executor})
		res := domain.VerificationCheckResult{CheckID: ch.CheckID, Required: ch.Required, Status: status, CheckSpecDigest: specDigest, CacheKey: cacheKey, EvidenceDigest: evidenceDigest, Message: message}
		results = append(results, res)
		resultByID[ch.CheckID] = res
	}
	outcome := Outcome(results)
	contractDigest, _ := domain.Digest(contract)
	verificationDigest, _ := domain.Digest(map[string]any{"format_version": "0.2", "transaction_id": transactionID, "contract_digest": contractDigest, "material_digest": materialDigest, "outcome": outcome, "results": results})
	return domain.VerificationReport{VerificationID: domain.NewID("verify"), TransactionID: transactionID, ContractID: contract.ContractID, ContractDigest: contractDigest, MaterialDigest: materialDigest, Outcome: outcome, Results: results, VerificationDigest: verificationDigest, PolicyVersion: contract.PolicyVersion, CreatedAt: time.Now().UTC()}, nil
}
func Outcome(results []domain.VerificationCheckResult) string {
	required := 0
	all := true
	for _, r := range results {
		if !r.Required {
			continue
		}
		required++
		if r.Status == "error" || r.Status == "cancelled" {
			return "error"
		}
		if r.Status != "pass" {
			all = false
		}
	}
	if required == 0 {
		return "error"
	}
	if all {
		return "pass"
	}
	return "fail"
}

func (e Engine) execute(transactionID string, workspace domain.Workspace, ch Check) (string, string, string, error) {
	switch ch.Executor {
	case "workspace_assertion":
		status, message, err := executeAssertion(workspace.WorkspacePath, ch)
		return status, message, "", err
	case "local_command":
		if !e.AllowLocalCommands {
			return "error", "local commands disabled", "", nil
		}
		status, message, err := executeCommand(workspace.WorkspacePath, ch)
		return status, message, "", err
	case "oci_command":
		return e.executeOCI(transactionID, workspace, ch)
	default:
		return "", "", "", errors.New("unsupported executor")
	}
}

func (e Engine) executeOCI(transactionID string, workspace domain.Workspace, ch Check) (string, string, string, error) {
	if e.OCI == nil {
		return "error", "OCI runtime is not configured", "", nil
	}
	runner := *e.OCI
	if ch.TimeoutSeconds > 0 {
		runner.Policy.Timeout = time.Duration(ch.TimeoutSeconds) * time.Second
	}
	executionID := domain.NewID("verifyexec")
	result, runErr := runner.Execute(context.Background(), runtimeoci.Request{TransactionID: transactionID, ExecutionID: executionID, Workspace: workspace.WorkspacePath, Command: ch.Command, Purpose: runtimeoci.Verification, SyncWorkspace: false})
	dir := filepath.Join(workspace.ArtifactsPath, "verification-runtime", ch.CheckID, executionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.log"), result.Stdout, 0o600); err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "stderr.log"), result.Stderr, 0o600); err != nil {
		return "", "", "", err
	}
	encoded, err := json.MarshalIndent(result.Evidence, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "evidence.json"), append(encoded, '\n'), 0o600); err != nil {
		return "", "", "", err
	}
	evidenceDigest, err := domain.Digest(result.Evidence)
	if err != nil {
		return "", "", "", err
	}
	message := truncate(string(result.CombinedOutput()), 4096)
	if runErr == nil {
		return "pass", message, evidenceDigest, nil
	}
	switch result.Evidence.TerminationReason {
	case runtimeoci.TimedOut:
		return "timeout", "command timed out", evidenceDigest, nil
	case runtimeoci.RuntimeError, runtimeoci.Cancelled:
		return "error", message, evidenceDigest, nil
	default:
		return "fail", message, evidenceDigest, nil
	}
}

func executeAssertion(root string, ch Check) (string, string, error) {
	p, err := safePath(root, ch.Path)
	if err != nil {
		return "", "", err
	}
	info, statErr := os.Lstat(p)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return "error", statErr.Error(), nil
	}
	if exists && info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("assertion target is symlink")
	}
	switch ch.Type {
	case "file_exists":
		if !exists {
			return "fail", "expected file to exist: " + ch.Path, nil
		}
	case "file_absent":
		if exists {
			return "fail", "expected file to be absent: " + ch.Path, nil
		}
	case "file_sha256":
		if !exists || !info.Mode().IsRegular() {
			return "fail", "expected regular file: " + ch.Path, nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "error", err.Error(), nil
		}
		if domain.SHA256Bytes(b) != strings.ToLower(ch.Digest) {
			return "fail", "SHA-256 mismatch: " + ch.Path, nil
		}
	}
	return "pass", "", nil
}
func safePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("invalid assertion path")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", errors.New("path traversal rejected")
	}
	parts := strings.Split(clean, string(filepath.Separator))
	for _, p := range parts {
		if p == ".git" {
			return "", errors.New(".git assertions rejected")
		}
	}
	current := root
	for i, p := range parts {
		current = filepath.Join(current, p)
		if i < len(parts)-1 {
			if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("symlinked path component rejected")
			}
		}
	}
	return current, nil
}
func executeCommand(root string, ch Check) (string, string, error) {
	timeout := time.Duration(ch.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ch.Command[0], ch.Command[1:]...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "timeout", "command timed out", nil
	}
	if err != nil {
		return "fail", truncate(string(out), 4096), nil
	}
	return "pass", truncate(string(out), 4096), nil
}
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
