// Package certsuite orchestrates host-specific certification checks without
// granting release authority or persisting credential material.
package certsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/certification"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/integrations"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Blocked Status = "blocked"
	Skip    Status = "skip"
)

type Check struct {
	ID       string         `json:"id"`
	Status   Status         `json:"status"`
	Required bool           `json:"required"`
	Detail   string         `json:"detail"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type TargetReport struct {
	Target    string  `json:"target"`
	Requested bool    `json:"requested"`
	Certified bool    `json:"certified"`
	Checks    []Check `json:"checks"`
}

type Report struct {
	FormatVersion string         `json:"format_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Build         buildinfo.Info `json:"build"`
	Host          HostInfo       `json:"host"`
	Targets       []TargetReport `json:"targets"`
	Certified     bool           `json:"certified"`
	ReportDigest  string         `json:"report_digest"`
}

type HostInfo struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

type Options struct {
	Targets []string

	ScratchRoot  string
	MCPBinary    string
	DaemonSocket string

	OCIRuntime string
	OCIBinary  string
	OCIImage   string

	GitHubOwner    string
	GitHubRepo     string
	GitHubTokenEnv string
	GitHubAPIBase  string

	SlackChannel  string
	SlackTokenEnv string
	SlackAPIBase  string

	OpenCodeBinary string
	HermesBinary   string

	GHBinary            string
	AttestationArtifact string
	AttestationRepo     string
}

type OCIExecutor interface {
	Ready(context.Context) (runtimeoci.Backend, error)
	Execute(context.Context, runtimeoci.Request) (runtimeoci.Result, error)
}

type Dependencies struct {
	OCI               OCIExecutor
	HTTPClientFactory func(egress.Policy) (*http.Client, error)
	CommandRunner     func(context.Context, string, []string, []string) ([]byte, error)
}

func Run(ctx context.Context, options Options, deps Dependencies) (Report, error) {
	targets, err := normalizeTargets(options.Targets)
	if err != nil {
		return Report{}, err
	}
	if deps.HTTPClientFactory == nil {
		deps.HTTPClientFactory = egress.NewClient
	}
	if deps.CommandRunner == nil {
		deps.CommandRunner = runCommand
	}
	report := Report{
		FormatVersion: "0.1",
		GeneratedAt:   time.Now().UTC(),
		Build:         buildinfo.Current(),
		Host:          HostInfo{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	}
	for _, target := range []string{"local", "oci", "github", "slack", "opencode", "hermes", "attestation"} {
		requested := targets[target]
		tr := TargetReport{Target: target, Requested: requested}
		if !requested {
			tr.Checks = []Check{{ID: "not_requested", Status: Skip, Detail: "target was not requested"}}
			report.Targets = append(report.Targets, tr)
			continue
		}
		switch target {
		case "local":
			tr.Checks = checkLocal(options)
		case "oci":
			tr.Checks = checkOCI(ctx, options, deps)
		case "github":
			tr.Checks = checkGitHub(ctx, options, deps)
		case "slack":
			tr.Checks = checkSlack(ctx, options, deps)
		case "opencode":
			tr.Checks = checkAgent(ctx, "opencode", options.OpenCodeBinary, options, deps)
		case "hermes":
			tr.Checks = checkAgent(ctx, "hermes", options.HermesBinary, options, deps)
		case "attestation":
			tr.Checks = checkAttestation(ctx, options, deps)
		}
		tr.Certified = targetCertified(tr.Checks)
		report.Targets = append(report.Targets, tr)
	}
	report.Certified = true
	for _, tr := range report.Targets {
		if tr.Requested && !tr.Certified {
			report.Certified = false
		}
	}
	clone := report
	clone.ReportDigest = ""
	digest, err := domain.Digest(clone)
	if err != nil {
		return Report{}, err
	}
	report.ReportDigest = digest
	return report, nil
}

func normalizeTargets(values []string) (map[string]bool, error) {
	allowed := map[string]bool{"local": true, "oci": true, "github": true, "slack": true, "opencode": true, "hermes": true, "attestation": true}
	result := map[string]bool{}
	if len(values) == 0 {
		result["local"] = true
		return result, nil
	}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "all" {
				for key := range allowed {
					result[key] = true
				}
				continue
			}
			if !allowed[item] {
				return nil, fmt.Errorf("unknown certification target %q", item)
			}
			result[item] = true
		}
	}
	return result, nil
}

func checkLocal(options Options) []Check {
	checks := []Check{}
	checks = append(checks, fileCheck("mcp_binary", options.MCPBinary, true))
	if strings.TrimSpace(options.DaemonSocket) == "" || !filepath.IsAbs(options.DaemonSocket) {
		checks = append(checks, Check{ID: "daemon_socket", Status: Fail, Required: true, Detail: "daemon socket must be an absolute path"})
	} else {
		checks = append(checks, Check{ID: "daemon_socket", Status: Pass, Required: true, Detail: "daemon socket path is absolute", Evidence: map[string]any{"path": options.DaemonSocket}})
	}
	if filepath.IsAbs(options.MCPBinary) && filepath.IsAbs(options.DaemonSocket) {
		oc, errOC := integrations.OpenCodeConfig(integrations.Options{MCPBinary: options.MCPBinary, Socket: options.DaemonSocket, Strict: true})
		he, errHE := integrations.HermesConfig(integrations.Options{MCPBinary: options.MCPBinary, Socket: options.DaemonSocket, Strict: true})
		checks = append(checks, resultCheck("opencode_profile_generation", errOC, "strict OpenCode profile generated", true))
		checks = append(checks, resultCheck("hermes_profile_generation", errHE, "strict Hermes profile generated", true))
		if errOC == nil && errHE == nil {
			lower := strings.ToLower(string(oc) + string(he))
			unsafe := []string{"transaction_approve", "transaction_commit", "credential_get", "credential_read"}
			leaked := ""
			for _, token := range unsafe {
				if strings.Contains(lower, token) {
					leaked = token
					break
				}
			}
			if leaked == "" {
				checks = append(checks, Check{ID: "release_authority_absent", Status: Pass, Required: true, Detail: "generated agent profiles contain no approval, commit, or credential tool"})
			} else {
				checks = append(checks, Check{ID: "release_authority_absent", Status: Fail, Required: true, Detail: "generated profile contains privileged tool: " + leaked})
			}
		}
	}
	return checks
}

func checkOCI(ctx context.Context, options Options, deps Dependencies) []Check {
	if options.OCIImage == "" {
		return []Check{{ID: "oci_prerequisites", Status: Blocked, Required: true, Detail: "--oci-image with a sha256 digest is required"}}
	}
	executor := deps.OCI
	if executor == nil {
		kind := runtimeoci.Docker
		if options.OCIRuntime == "podman" {
			kind = runtimeoci.Podman
		}
		executor = &runtimeoci.Runner{Kind: kind, Binary: options.OCIBinary, Policy: runtimeoci.DefaultPolicy(options.OCIImage), ScratchRoot: filepath.Join(nonEmpty(options.ScratchRoot, os.TempDir()), "oci")}
	}
	r, err := certification.Run(ctx, executor, options.OCIImage, certification.Options{Root: nonEmpty(options.ScratchRoot, os.TempDir())})
	if err != nil {
		return []Check{{ID: "oci_certification", Status: Fail, Required: true, Detail: err.Error()}}
	}
	checks := make([]Check, 0, len(r.Checks))
	for _, c := range r.Checks {
		status := Pass
		if c.Status == certification.Fail {
			status = Fail
		}
		if c.Status == certification.Skip {
			status = Skip
		}
		checks = append(checks, Check{ID: c.ID, Status: status, Required: c.Required, Detail: c.Detail})
	}
	return checks
}

func checkGitHub(ctx context.Context, options Options, deps Dependencies) []Check {
	if options.GitHubOwner == "" || options.GitHubRepo == "" || options.GitHubTokenEnv == "" {
		return []Check{{ID: "github_prerequisites", Status: Blocked, Required: true, Detail: "GitHub owner, repo, and token environment-variable name are required"}}
	}
	token := []byte(os.Getenv(options.GitHubTokenEnv))
	if len(token) == 0 {
		return []Check{{ID: "github_token", Status: Blocked, Required: true, Detail: "configured GitHub token environment variable is empty"}}
	}
	defer zero(token)
	base := nonEmpty(options.GitHubAPIBase, "https://api.github.com")
	rule, err := egress.RuleFromBase(base, http.MethodGet)
	if err != nil {
		return []Check{{ID: "github_egress_policy", Status: Fail, Required: true, Detail: err.Error()}}
	}
	client, err := deps.HTTPClientFactory(egress.Policy{Rules: []egress.Rule{rule}})
	if err != nil {
		return []Check{{ID: "github_egress_client", Status: Fail, Required: true, Detail: err.Error()}}
	}
	endpoint := strings.TrimRight(base, "/") + "/repos/" + options.GitHubOwner + "/" + options.GitHubRepo
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return []Check{{ID: "github_repository_read", Status: Fail, Required: true, Detail: safeError(err)}}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []Check{{ID: "github_repository_read", Status: Fail, Required: true, Detail: fmt.Sprintf("GitHub returned HTTP %d", resp.StatusCode)}}
	}
	var body struct {
		FullName    string          `json:"full_name"`
		Permissions map[string]bool `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return []Check{{ID: "github_repository_read", Status: Fail, Required: true, Detail: "invalid GitHub response: " + err.Error()}}
	}
	want := options.GitHubOwner + "/" + options.GitHubRepo
	checks := []Check{{ID: "github_repository_read", Status: boolStatus(strings.EqualFold(body.FullName, want)), Required: true, Detail: fmt.Sprintf("repository identity=%s", body.FullName)}}
	push, known := body.Permissions["push"]
	if !known {
		checks = append(checks, Check{ID: "github_push_permission", Status: Blocked, Required: true, Detail: "GitHub response did not include push permission metadata"})
	} else if push {
		checks = append(checks, Check{ID: "github_push_permission", Status: Pass, Required: true, Detail: "token reports push access to the dedicated test repository"})
	} else {
		checks = append(checks, Check{ID: "github_push_permission", Status: Fail, Required: true, Detail: "token lacks push access to the dedicated test repository"})
	}
	checks = append(checks, Check{ID: "github_mutation_certification", Status: Blocked, Required: false, Detail: "readiness passed; create-only branch and draft-PR mutation certification requires an explicit disposable test run"})
	return checks
}

func checkSlack(ctx context.Context, options Options, deps Dependencies) []Check {
	if options.SlackChannel == "" || options.SlackTokenEnv == "" {
		return []Check{{ID: "slack_prerequisites", Status: Blocked, Required: true, Detail: "Slack channel and token environment-variable name are required"}}
	}
	token := []byte(os.Getenv(options.SlackTokenEnv))
	if len(token) == 0 {
		return []Check{{ID: "slack_token", Status: Blocked, Required: true, Detail: "configured Slack token environment variable is empty"}}
	}
	defer zero(token)
	base := nonEmpty(options.SlackAPIBase, "https://slack.com/api")
	rule, err := egress.RuleFromBase(base, http.MethodPost)
	if err != nil {
		return []Check{{ID: "slack_egress_policy", Status: Fail, Required: true, Detail: err.Error()}}
	}
	client, err := deps.HTTPClientFactory(egress.Policy{Rules: []egress.Rule{rule}})
	if err != nil {
		return []Check{{ID: "slack_egress_client", Status: Fail, Required: true, Detail: err.Error()}}
	}
	authOK, authDetail := slackPOST(ctx, client, strings.TrimRight(base, "/")+"/auth.test", token, "")
	channelOK, channelDetail := slackPOST(ctx, client, strings.TrimRight(base, "/")+"/conversations.info", token, "channel="+options.SlackChannel)
	return []Check{
		{ID: "slack_auth", Status: boolStatus(authOK), Required: true, Detail: authDetail},
		{ID: "slack_channel_read", Status: boolStatus(channelOK), Required: true, Detail: channelDetail},
		{ID: "slack_chat_write_certification", Status: Blocked, Required: false, Detail: "readiness passed; exactly-once message mutation certification requires an explicit disposable channel run"},
	}
}

func slackPOST(ctx context.Context, client *http.Client, endpoint string, token []byte, body string) (bool, string) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return false, safeError(err)
	}
	defer resp.Body.Close()
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "invalid Slack response: " + err.Error()
	}
	if !result.OK {
		return false, "Slack rejected request: " + result.Error
	}
	return true, "Slack API check passed"
}

func checkAgent(ctx context.Context, target, binary string, options Options, deps Dependencies) []Check {
	if strings.TrimSpace(binary) == "" {
		return []Check{{ID: target + "_binary", Status: Blocked, Required: true, Detail: "agent binary path was not provided"}}
	}
	checks := []Check{fileCheck(target+"_binary", binary, true)}
	if checks[0].Status != Pass {
		return checks
	}
	out, err := deps.CommandRunner(ctx, binary, []string{"--version"}, minimalEnv())
	checks = append(checks, resultCheck(target+"_version", err, strings.TrimSpace(string(out)), true))
	if options.MCPBinary == "" || options.DaemonSocket == "" {
		checks = append(checks, Check{ID: target + "_profile", Status: Blocked, Required: true, Detail: "MCP binary and daemon socket are required"})
		return checks
	}
	var config []byte
	if target == "opencode" {
		config, err = integrations.OpenCodeConfig(integrations.Options{MCPBinary: options.MCPBinary, Socket: options.DaemonSocket, Strict: true})
	} else {
		config, err = integrations.HermesConfig(integrations.Options{MCPBinary: options.MCPBinary, Socket: options.DaemonSocket, Strict: true})
	}
	checks = append(checks, resultCheck(target+"_profile", err, "strict FutureDiff MCP profile generated", true))
	if err == nil && (strings.Contains(strings.ToLower(string(config)), "transaction_commit") || strings.Contains(strings.ToLower(string(config)), "transaction_approve")) {
		checks = append(checks, Check{ID: target + "_release_authority", Status: Fail, Required: true, Detail: "profile exposes release authority"})
	} else if err == nil {
		checks = append(checks, Check{ID: target + "_release_authority", Status: Pass, Required: true, Detail: "profile excludes approval and commit authority"})
	}
	checks = append(checks, Check{ID: target + "_live_transaction", Status: Blocked, Required: false, Detail: "binary and profile are ready; a complete live-agent transaction requires an explicit disposable repository run"})
	return checks
}

func checkAttestation(ctx context.Context, options Options, deps Dependencies) []Check {
	if options.AttestationArtifact == "" || options.AttestationRepo == "" {
		return []Check{{ID: "attestation_prerequisites", Status: Blocked, Required: true, Detail: "artifact path and GitHub repository are required"}}
	}
	gh := nonEmpty(options.GHBinary, "gh")
	if _, err := exec.LookPath(gh); err != nil {
		return []Check{{ID: "gh_binary", Status: Blocked, Required: true, Detail: "GitHub CLI was not found"}}
	}
	out, err := deps.CommandRunner(ctx, gh, []string{"attestation", "verify", options.AttestationArtifact, "--repo", options.AttestationRepo}, minimalEnv())
	if err != nil {
		return []Check{{ID: "signed_attestation", Status: Fail, Required: true, Detail: safeError(fmt.Errorf("%w: %s", err, truncate(string(out), 512)))}}
	}
	return []Check{{ID: "signed_attestation", Status: Pass, Required: true, Detail: "GitHub artifact attestation verified", Evidence: map[string]any{"repository": options.AttestationRepo, "artifact": filepath.Base(options.AttestationArtifact)}}}
}

func targetCertified(checks []Check) bool {
	for _, c := range checks {
		if c.Required && c.Status != Pass {
			return false
		}
	}
	return true
}

func fileCheck(id, path string, required bool) Check {
	if !filepath.IsAbs(path) {
		return Check{ID: id, Status: Fail, Required: required, Detail: "path must be absolute"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return Check{ID: id, Status: Fail, Required: required, Detail: err.Error()}
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return Check{ID: id, Status: Fail, Required: required, Detail: "path is not an executable file"}
	}
	return Check{ID: id, Status: Pass, Required: required, Detail: "executable exists", Evidence: map[string]any{"path": path}}
}

func resultCheck(id string, err error, success string, required bool) Check {
	if err != nil {
		return Check{ID: id, Status: Fail, Required: required, Detail: safeError(err)}
	}
	return Check{ID: id, Status: Pass, Required: required, Detail: nonEmpty(success, "check passed")}
}

func boolStatus(ok bool) Status {
	if ok {
		return Pass
	}
	return Fail
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
func zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	for _, marker := range []string{"Bearer ", "token=", "access_token="} {
		if idx := strings.Index(strings.ToLower(value), strings.ToLower(marker)); idx >= 0 {
			value = value[:idx] + "[REDACTED]"
		}
	}
	return truncate(value, 1024)
}
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
func minimalEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + nonEmpty(os.Getenv("HOME"), os.TempDir()), "LANG=C", "LC_ALL=C"}
}
func runCommand(ctx context.Context, binary string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func WriteJSON(path string, report Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Summary(report Report) string {
	counts := map[Status]int{}
	for _, target := range report.Targets {
		for _, c := range target.Checks {
			counts[c.Status]++
		}
	}
	keys := []Status{Pass, Fail, Blocked, Skip}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	parts := []string{}
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, " ")
}
