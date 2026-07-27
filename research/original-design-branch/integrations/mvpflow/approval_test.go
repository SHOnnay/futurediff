package mvpflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
)

func TestCommitApprovedBlocksInvalidatedApproval(t *testing.T) {
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
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'approval future\n' > approval.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'approval future' approval.txt"},
		MigrationUpSQL:   `CREATE TABLE approval_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE approval_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Approval widgets", Head: "agent/approval-widgets", Base: "main", BaseSHA: "sha_old", Body: "Prepared by FutureDiff", EffectID: "eff_pr_approval"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared approval widgets", EffectID: "eff_slack_approval"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	approval, err := CaptureApproval(prepared, "human_required", "approved", "policy-0.1", "policy_hash", "verification_hash")
	if err != nil {
		t.Fatalf("capture approval: %v", err)
	}
	mutated := *prepared
	mutated.GitHubPrepared = service.GitHubClient.Prepare(prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Approval widgets", Head: "agent/approval-widgets", Base: "main", BaseSHA: "sha_new", Body: "Prepared by FutureDiff", EffectID: "eff_pr_approval"})

	if _, err := service.CommitApproved(context.Background(), repo, &mutated, approval); err == nil || err.Error() == "" {
		t.Fatal("expected invalidated approval to block commit")
	}
	if _, err := os.Stat(filepath.Join(repo, "approval.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected repo to remain unchanged, got err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if githubCalls != 0 {
		t.Fatalf("expected zero github calls, got %d", githubCalls)
	}
	if slackCalls != 0 {
		t.Fatalf("expected zero slack calls, got %d", slackCalls)
	}
}

func TestValidateApprovalAcceptsMatchingPreparedState(t *testing.T) {
	repo := initGitRepo(t)
	service := Service{}
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'valid approval future\n' > valid-approval.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'valid approval future' valid-approval.txt"},
		MigrationUpSQL:   `CREATE TABLE valid_approval_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE valid_approval_widgets;`,
		EvidenceDir:      t.TempDir(),
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Valid approval widgets", Head: "agent/valid-approval-widgets", Base: "main", BaseSHA: "sha_main", Body: "Prepared by FutureDiff", EffectID: "eff_pr_valid_approval"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared valid approval widgets", EffectID: "eff_slack_valid_approval"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	approval, err := CaptureApproval(prepared, "human_required", "approved", "policy-0.1", "policy_hash", "verification_hash")
	if err != nil {
		t.Fatalf("capture approval: %v", err)
	}
	validation, err := ValidateApproval(approval, prepared)
	if err != nil {
		t.Fatalf("validate approval: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("expected approval to remain valid, got %s", validation.Reason)
	}
}
