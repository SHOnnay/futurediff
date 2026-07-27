package certification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/futurepack"
)

const (
	GitHubTokenEnv = "FUTUREDIFF_GITHUB_TOKEN"
	SlackTokenEnv  = "FUTUREDIFF_SLACK_TOKEN"
)

type ProviderSmokeConfig struct {
	GitHubBaseURL     string
	GitHubHTTPClient  *http.Client
	GitHubToken       string
	GitHubOwner       string
	GitHubRepo        string
	GitHubBaseBranch  string
	GitHubExpectedSHA string
	SlackBaseURL      string
	SlackHTTPClient   *http.Client
	SlackToken        string
	SlackChannel      string
	SlackText         string
	SlackThreadTS     string
	SlackEffectID     string
}

type ProviderReport struct {
	FormatVersion        string    `json:"format_version"`
	GeneratedAt          time.Time `json:"generated_at"`
	GitHubOwner          string    `json:"github_owner"`
	GitHubRepo           string    `json:"github_repo"`
	GitHubBaseBranch     string    `json:"github_base_branch"`
	GitHubCurrentBaseSHA string    `json:"github_current_base_sha"`
	SlackChannel         string    `json:"slack_channel"`
	SlackTimestamp       string    `json:"slack_timestamp"`
	Checks               []Check   `json:"checks"`
	Certified            bool      `json:"certified"`
	ReportDigest         string    `json:"report_digest"`
}

type ProviderOptions struct {
	Root           string
	FuturepackPath string
}

func LoadProviderConfigFromEnv() ProviderSmokeConfig {
	return ProviderSmokeConfig{
		GitHubToken: strings.TrimSpace(os.Getenv(GitHubTokenEnv)),
		SlackToken:  strings.TrimSpace(os.Getenv(SlackTokenEnv)),
	}
}

func RunProviderSmoke(ctx context.Context, cfg ProviderSmokeConfig, options ProviderOptions) (ProviderReport, error) {
	if err := cfg.Validate(); err != nil {
		return ProviderReport{}, err
	}
	report := ProviderReport{
		FormatVersion:    "0.1",
		GeneratedAt:      time.Now().UTC(),
		GitHubOwner:      cfg.GitHubOwner,
		GitHubRepo:       cfg.GitHubRepo,
		GitHubBaseBranch: cfg.GitHubBaseBranch,
		SlackChannel:     cfg.SlackChannel,
	}

	currentSHA, err := queryGitHubBranchSHA(ctx, cfg)
	if err != nil {
		report.Checks = append(report.Checks,
			Check{ID: "github_branch_query", Status: Fail, Detail: err.Error(), Required: true},
			Check{ID: "github_stale_base_detected", Status: Fail, Detail: "branch freshness could not be evaluated", Required: true},
		)
		return finalizeProvider(report), nil
	}
	report.GitHubCurrentBaseSHA = currentSHA
	report.Checks = append(report.Checks,
		Check{ID: "github_branch_query", Status: Pass, Detail: fmt.Sprintf("queried %s/%s branch %s", cfg.GitHubOwner, cfg.GitHubRepo, cfg.GitHubBaseBranch), Required: true},
		booleanCheck("github_stale_base_detected", currentSHA != cfg.GitHubExpectedSHA, fmt.Sprintf("current SHA %s differs from expected stale SHA %s", currentSHA, cfg.GitHubExpectedSHA), fmt.Sprintf("current SHA %s unexpectedly matched expected stale SHA", currentSHA), true),
	)

	slackAdapter := &slackoutbox.Adapter{BaseURL: cfg.SlackBaseURL, HTTPClient: cfg.SlackHTTPClient}
	prepared, _, err := slackAdapter.Prepare(defaultString(cfg.SlackEffectID, "provider-smoke-slack"), slackoutbox.Input{Channel: cfg.SlackChannel, Text: cfg.SlackText})
	if err != nil {
		report.Checks = append(report.Checks,
			Check{ID: "slack_prepare", Status: Fail, Detail: err.Error(), Required: true},
			Check{ID: "slack_post", Status: Fail, Detail: "message was not posted", Required: true},
			Check{ID: "slack_recovery", Status: Fail, Detail: "message recovery was not attempted", Required: true},
		)
		return finalizeProvider(report), nil
	}
	report.Checks = append(report.Checks, Check{ID: "slack_prepare", Status: Pass, Detail: "prepared exact Slack payload with metadata marker", Required: true})

	receipt, err := slackAdapter.Post(ctx, prepared, []byte(cfg.SlackToken))
	if err != nil {
		report.Checks = append(report.Checks,
			Check{ID: "slack_post", Status: Fail, Detail: err.Error(), Required: true},
			Check{ID: "slack_recovery", Status: Fail, Detail: "message recovery was not attempted", Required: true},
		)
		return finalizeProvider(report), nil
	}
	report.SlackTimestamp = receipt.Timestamp
	report.Checks = append(report.Checks, booleanCheck("slack_post", receipt.Timestamp != "", fmt.Sprintf("posted Slack message at %s", receipt.Timestamp), "Slack receipt did not include a timestamp", true))

	status, err := slackAdapter.Status(ctx, prepared, []byte(cfg.SlackToken))
	if err != nil {
		report.Checks = append(report.Checks, Check{ID: "slack_recovery", Status: Fail, Detail: err.Error(), Required: true})
		return finalizeProvider(report), nil
	}
	recovered := status.Status == slackoutbox.StatusCommitted && status.Receipt != nil
	if report.SlackTimestamp == "" && recovered {
		report.SlackTimestamp = status.Receipt.Timestamp
	}
	report.Checks = append(report.Checks, booleanCheck("slack_recovery", recovered, "Slack history lookup recovered the posted message by deterministic marker", fmt.Sprintf("Slack status returned %s", status.Status), true))

	report = finalizeProvider(report)
	if strings.TrimSpace(options.FuturepackPath) != "" {
		if err := exportProviderFuturepack(report, options); err != nil {
			return ProviderReport{}, err
		}
	}
	return report, nil
}

func (cfg ProviderSmokeConfig) Validate() error {
	checks := map[string]string{
		"github token":        cfg.GitHubToken,
		"github owner":        cfg.GitHubOwner,
		"github repo":         cfg.GitHubRepo,
		"github base branch":  cfg.GitHubBaseBranch,
		"github expected sha": cfg.GitHubExpectedSHA,
		"slack token":         cfg.SlackToken,
		"slack channel":       cfg.SlackChannel,
		"slack text":          cfg.SlackText,
	}
	for label, value := range checks {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
	}
	return nil
}

func queryGitHubBranchSHA(ctx context.Context, cfg ProviderSmokeConfig) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.GitHubBaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/branches/%s", base, cfg.GitHubOwner, cfg.GitHubRepo, cfg.GitHubBaseBranch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	resp, err := providerHTTPClient(cfg.GitHubHTTPClient).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub branch query HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Commit.SHA) == "" {
		return "", errors.New("GitHub branch query returned empty sha")
	}
	return strings.ToLower(strings.TrimSpace(payload.Commit.SHA)), nil
}

func exportProviderFuturepack(report ProviderReport, options ProviderOptions) error {
	root := options.Root
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(filepath.Dir(options.FuturepackPath), "provider-smoke-artifacts")
	}
	store, err := futurepack.Open(root)
	if err != nil {
		return err
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	reportRef, err := store.PutBytes("provider-smoke-report.json", append(reportBytes, '\n'))
	if err != nil {
		return err
	}
	manifest := futurepack.Manifest{
		FormatVersion: "0.1",
		TransactionID: fmt.Sprintf("provider-smoke-%d", time.Now().UnixNano()),
		Scenario:      "provider_smoke",
		Verdict:       verdictFromCertified(report.Certified),
		Metadata: map[string]any{
			"github_owner":       report.GitHubOwner,
			"github_repo":        report.GitHubRepo,
			"github_base_branch": report.GitHubBaseBranch,
			"slack_channel":      report.SlackChannel,
		},
		Artifacts: []futurepack.Ref{reportRef},
	}
	return store.Export(options.FuturepackPath, manifest)
}

func finalizeProvider(report ProviderReport) ProviderReport {
	report.Certified = true
	for _, check := range report.Checks {
		if check.Required && check.Status != Pass {
			report.Certified = false
		}
	}
	clone := report
	clone.ReportDigest = ""
	digest, _ := domain.Digest(clone)
	report.ReportDigest = digest
	return report
}

func verdictFromCertified(certified bool) string {
	if certified {
		return "pass"
	}
	return "fail"
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func providerHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func WriteProviderJSON(path string, report ProviderReport) error {
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
