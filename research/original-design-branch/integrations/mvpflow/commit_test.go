package mvpflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
)

func TestCommitOrchestratesRepoGitHubSlackWithoutRerun(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	var (
		mu    sync.Mutex
		calls []string
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			calls = append(calls, "github-freshness")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "sha_main"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			calls = append(calls, "github-create")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 900, "html_url": "https://github.example/acme/payments/pull/900"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, "slack-send")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C123", "ts": "1712345.100000"})
	}))
	defer slackServer.Close()

	service := Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL},
	}
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'committed future\n' > commit.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'committed future' commit.txt"},
		MigrationUpSQL:   `CREATE TABLE commit_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE commit_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Commit widgets", Head: "agent/commit-widgets", Base: "main", BaseSHA: "sha_main", Body: "Prepared by FutureDiff", EffectID: "eff_pr_commit"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared commit widgets", EffectID: "eff_slack_commit"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(prepared.Transaction.WorktreePath); err != nil {
		t.Fatalf("expected worktree before removal: %v", err)
	}
	if err := os.RemoveAll(prepared.Transaction.WorktreePath); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	committed, err := service.Commit(context.Background(), repo, prepared)
	if err != nil {
		t.Fatalf("commit orchestration: %v", err)
	}
	if committed.Transaction.State != "COMMITTED" {
		t.Fatalf("expected committed transaction, got %s", committed.Transaction.State)
	}
	if committed.GitHubReceipt == nil || committed.GitHubReceipt.PullNumber != 900 {
		t.Fatalf("unexpected github receipt: %#v", committed.GitHubReceipt)
	}
	if committed.SlackReceipt == nil || committed.SlackReceipt.TS == "" {
		t.Fatalf("unexpected slack receipt: %#v", committed.SlackReceipt)
	}
	bytes, err := os.ReadFile(filepath.Join(repo, "commit.txt"))
	if err != nil {
		t.Fatalf("read committed file: %v", err)
	}
	if string(bytes) != "committed future\n" {
		t.Fatalf("unexpected committed content: %q", string(bytes))
	}
	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	joined := strings.Join(gotCalls, ",")
	if joined != "github-freshness,github-create,slack-send" {
		t.Fatalf("unexpected external call order: %s", joined)
	}
}

func TestCommitRecoversAmbiguousExternalEffects(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	var (
		mu       sync.Mutex
		pulls    []map[string]any
		messages []map[string]any
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "sha_main"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			pulls = append(pulls, map[string]any{"number": 901, "html_url": "https://github.example/acme/payments/pull/901", "title": payload["title"], "body": payload["body"], "head": map[string]any{"ref": payload["head"]}, "base": map[string]any{"ref": payload["base"]}})
			mu.Unlock()
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pulls[len(pulls)-1])
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pulls)
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat.postMessage":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			messages = append(messages, map[string]any{"text": payload["text"], "ts": "1712345.100001", "metadata": payload["metadata"]})
			mu.Unlock()
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": payload["channel"], "ts": "1712345.100001"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/conversations.history":
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": messages})
		default:
			http.NotFound(w, r)
		}
	}))
	defer slackServer.Close()

	service := Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}},
	}
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'recoverable commit future\n' > recover-commit.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'recoverable commit future' recover-commit.txt"},
		MigrationUpSQL:   `CREATE TABLE recover_commit_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE recover_commit_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Recover commit widgets", Head: "agent/recover-commit-widgets", Base: "main", BaseSHA: "sha_main", Body: "Prepared by FutureDiff", EffectID: "eff_pr_commit_recover"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared recover commit widgets", EffectID: "eff_slack_commit_recover"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	committed, err := service.Commit(context.Background(), repo, prepared)
	if err != nil {
		t.Fatalf("commit orchestration recovery: %v", err)
	}
	if committed.GitHubReceipt == nil || !committed.GitHubReceipt.Recovered {
		t.Fatalf("expected recovered github receipt, got %#v", committed.GitHubReceipt)
	}
	if committed.SlackReceipt == nil || !committed.SlackReceipt.Recovered {
		t.Fatalf("expected recovered slack receipt, got %#v", committed.SlackReceipt)
	}
}

func TestReconcileCommitAfterCrashPostGitHubReceipt(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	var (
		mu                sync.Mutex
		githubCalls       int
		slackSendCalls    int
		slackHistoryCalls int
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "sha_main"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			githubCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 902, "html_url": "https://github.example/acme/payments/pull/902"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat.postMessage":
			mu.Lock()
			slackSendCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C123", "ts": "1712345.100002"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/conversations.history":
			mu.Lock()
			slackHistoryCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer slackServer.Close()

	service := Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL},
	}
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'reconcile future\n' > reconcile.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'reconcile future' reconcile.txt"},
		MigrationUpSQL:   `CREATE TABLE reconcile_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE reconcile_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Reconcile widgets", Head: "agent/reconcile-widgets", Base: "main", BaseSHA: "sha_main", Body: "Prepared by FutureDiff", EffectID: "eff_pr_reconcile"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared reconcile widgets", EffectID: "eff_slack_reconcile"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	crashing := service
	crashing.AfterGitHubReceipt = func(record *CommitRecord) error {
		return os.ErrProcessDone
	}
	if _, err := crashing.Commit(context.Background(), repo, prepared); err == nil {
		t.Fatal("expected simulated crash after github receipt")
	}
	mu.Lock()
	if githubCalls != 1 {
		mu.Unlock()
		t.Fatalf("expected one github create before reconcile, got %d", githubCalls)
	}
	if slackSendCalls != 0 {
		mu.Unlock()
		t.Fatalf("expected zero slack sends before reconcile, got %d", slackSendCalls)
	}
	mu.Unlock()

	reconciled, err := service.ReconcileCommit(context.Background(), repo, prepared)
	if err != nil {
		t.Fatalf("reconcile commit: %v", err)
	}
	if reconciled.Transaction.State != "COMMITTED" {
		t.Fatalf("expected committed transaction after reconcile, got %s", reconciled.Transaction.State)
	}
	if reconciled.GitHubReceipt == nil || reconciled.GitHubReceipt.PullNumber != 902 {
		t.Fatalf("unexpected github receipt after reconcile: %#v", reconciled.GitHubReceipt)
	}
	if reconciled.SlackReceipt == nil || reconciled.SlackReceipt.TS == "" {
		t.Fatalf("unexpected slack receipt after reconcile: %#v", reconciled.SlackReceipt)
	}
	mu.Lock()
	defer mu.Unlock()
	if githubCalls != 1 {
		t.Fatalf("expected no duplicate github create during reconcile, got %d", githubCalls)
	}
	if slackHistoryCalls != 1 {
		t.Fatalf("expected one slack recovery lookup during reconcile, got %d", slackHistoryCalls)
	}
	if slackSendCalls != 1 {
		t.Fatalf("expected one slack send during reconcile, got %d", slackSendCalls)
	}
}

func TestCommitWithCompensationClosesGitHubAfterSlackFailure(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	var (
		mu            sync.Mutex
		githubCreates int
		githubCloses  int
		slackSends    int
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "sha_main"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			githubCreates++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 903, "html_url": "https://github.example/acme/payments/pull/903"})
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/payments/pulls/903":
			mu.Lock()
			githubCloses++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 903, "html_url": "https://github.example/acme/payments/pull/903", "state": "closed"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat.postMessage":
			mu.Lock()
			slackSends++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/conversations.history":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer slackServer.Close()

	service := Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL},
	}
	prepared, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'compensated future\n' > compensated.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'compensated future' compensated.txt"},
		MigrationUpSQL:   `CREATE TABLE compensated_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE compensated_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest:    prcreate.CreateRequest{Owner: "acme", Repo: "payments", Title: "Compensated widgets", Head: "agent/compensated-widgets", Base: "main", BaseSHA: "sha_main", Body: "Prepared by FutureDiff", EffectID: "eff_pr_compensated"},
		SlackRequest:     outbox.SendRequest{Channel: "C123", Text: "Prepared compensated widgets", EffectID: "eff_slack_compensated"},
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	result, err := service.CommitWithCompensation(context.Background(), repo, prepared)
	if err == nil {
		t.Fatal("expected compensation error")
	}
	if _, ok := err.(*CompensationError); !ok {
		t.Fatalf("expected compensation error type, got %T", err)
	}
	if result == nil || result.GitHubCompensation == nil {
		t.Fatalf("expected compensation result, got %#v", result)
	}
	if result.CompensationState != "COMPENSATED" {
		t.Fatalf("expected compensated state, got %s", result.CompensationState)
	}
	if result.GitHubCompensation.State != "closed" {
		t.Fatalf("expected closed github compensation state, got %#v", result.GitHubCompensation)
	}
	bytes, err := os.ReadFile(filepath.Join(repo, "compensated.txt"))
	if err != nil {
		t.Fatalf("read compensated file: %v", err)
	}
	if string(bytes) != "compensated future\n" {
		t.Fatalf("unexpected compensated repo content: %q", string(bytes))
	}
	mu.Lock()
	defer mu.Unlock()
	if githubCreates != 1 {
		t.Fatalf("expected one github create, got %d", githubCreates)
	}
	if githubCloses != 1 {
		t.Fatalf("expected one github close compensation, got %d", githubCloses)
	}
	if slackSends != 1 {
		t.Fatalf("expected one failed slack send, got %d", slackSends)
	}
}
