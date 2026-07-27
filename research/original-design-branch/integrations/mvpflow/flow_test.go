package mvpflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
)

func TestPrepareSuccessAcrossRepoPostgresGitHubSlack(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	service := Service{}

	result, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'vertical slice success\n' > success.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'vertical slice success' success.txt"},
		MigrationUpSQL:   `CREATE TABLE accounts (id BIGSERIAL PRIMARY KEY, status TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE accounts;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest: prcreate.CreateRequest{
			Owner:    "acme",
			Repo:     "payments",
			Title:    "Add accounts table",
			Head:     "agent/accounts-table",
			Base:     "main",
			Body:     "Prepared by FutureDiff",
			EffectID: "eff_pr_success",
		},
		SlackRequest: outbox.SendRequest{
			Channel:  "C123",
			Text:     "Prepared accounts table change",
			EffectID: "eff_slack_success",
		},
	})
	if err != nil {
		t.Fatalf("prepare vertical slice: %v", err)
	}
	if result.Transaction.State != "AWAITING_APPROVAL" {
		t.Fatalf("expected awaiting approval, got %s", result.Transaction.State)
	}
	if !strings.Contains(result.StagedPatch, "vertical slice success") {
		t.Fatalf("expected staged patch content, got %q", result.StagedPatch)
	}
	if result.PostgresPreview == nil || !result.PostgresPreview.RollbackVerified {
		t.Fatal("expected rollback-verified postgres preview")
	}
	if result.GitHubPrepared.SupportLevel != prcreate.SupportLevelPreviewWithFreshnessCheck {
		t.Fatalf("unexpected github support level: %s", result.GitHubPrepared.SupportLevel)
	}
	if !strings.Contains(result.GitHubPrepared.PreviewBody, "FutureDiff-Effect: eff_pr_success") {
		t.Fatalf("expected github preview marker, got %q", result.GitHubPrepared.PreviewBody)
	}
	if result.SlackPrepared.SupportLevel != outbox.SupportLevelIdempotentBestEffort {
		t.Fatalf("unexpected slack support level: %s", result.SlackPrepared.SupportLevel)
	}
	metadata := result.SlackPrepared.Payload["metadata"].(map[string]any)
	eventPayload := metadata["event_payload"].(map[string]any)
	if eventPayload["effect_id"] != "eff_slack_success" {
		t.Fatalf("expected slack effect marker, got %#v", eventPayload)
	}
	if result.Transaction.VerificationOutputPath == "" {
		t.Fatal("expected verification output path")
	}
	if _, err := os.Stat(result.Transaction.VerificationOutputPath); err != nil {
		t.Fatalf("expected verification evidence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "success.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source repo to remain unchanged before commit, got err=%v", err)
	}
}

func TestPrepareFailureLeavesZeroOutwardSideEffects(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()

	var (
		mu          sync.Mutex
		githubCalls int
		slackCalls  int
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		githubCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		slackCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	service := Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL},
	}

	result, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'vertical slice failure\n' > failure.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "printf 'verification failed\n' >&2; exit 1"},
		MigrationUpSQL:   `CREATE TABLE audit_log (id BIGSERIAL PRIMARY KEY, body TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE audit_log;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest: prcreate.CreateRequest{
			Owner:    "acme",
			Repo:     "payments",
			Title:    "Add audit log table",
			Head:     "agent/audit-log-table",
			Base:     "main",
			Body:     "Prepared by FutureDiff",
			EffectID: "eff_pr_failure",
		},
		SlackRequest: outbox.SendRequest{
			Channel:  "C123",
			Text:     "Prepared audit log change",
			EffectID: "eff_slack_failure",
		},
	})
	if err != nil {
		t.Fatalf("prepare failure vertical slice: %v", err)
	}
	if result.Transaction.State != "ABORTED" {
		t.Fatalf("expected aborted state, got %s", result.Transaction.State)
	}
	if !strings.Contains(result.StagedPatch, "vertical slice failure") {
		t.Fatalf("expected staged patch to remain inspectable, got %q", result.StagedPatch)
	}
	if result.PostgresPreview == nil || !result.PostgresPreview.RollbackVerified {
		t.Fatal("expected postgres preview evidence even on failure path")
	}
	if _, err := os.Stat(filepath.Join(repo, "failure.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source repo to remain unchanged, got err=%v", err)
	}
	verificationBytes, err := os.ReadFile(result.Transaction.VerificationOutputPath)
	if err != nil {
		t.Fatalf("read verification evidence: %v", err)
	}
	if !strings.Contains(string(verificationBytes), "verification failed") {
		t.Fatalf("expected verification failure evidence, got %q", string(verificationBytes))
	}

	mu.Lock()
	defer mu.Unlock()
	if githubCalls != 0 {
		t.Fatalf("expected zero github network calls, got %d", githubCalls)
	}
	if slackCalls != 0 {
		t.Fatalf("expected zero slack network calls, got %d", slackCalls)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	mustGit(t, repo, "init")
	mustGit(t, repo, "config", "user.email", "slice@example.com")
	mustGit(t, repo, "config", "user.name", "Slice User")
	mustGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	return repo
}

func mustGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(output)))
	}
}
