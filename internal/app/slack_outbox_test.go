package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type fakeSlack struct {
	mu        sync.Mutex
	messages  []map[string]any
	posts     int
	ambiguous bool
	token     string
}

func (f *fakeSlack) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		return appResponse(401, `{"ok":false,"error":"unauthorized"}`), nil
	}
	if strings.HasSuffix(r.URL.Path, "conversations.history") {
		b, _ := json.Marshal(map[string]any{"ok": true, "messages": f.messages})
		return appResponse(200, string(b)), nil
	}
	if strings.HasSuffix(r.URL.Path, "chat.postMessage") {
		f.posts++
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		m := map[string]any{"ts": "1700.1", "client_msg_id": p["client_msg_id"], "metadata": p["metadata"]}
		f.messages = append(f.messages, m)
		if f.ambiguous {
			return nil, errors.New("reset after accept")
		}
		return appResponse(200, `{"ok":true,"channel":"C12345678","ts":"1700.1","message":{"client_msg_id":"`+p["client_msg_id"].(string)+`"}}`), nil
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
}

func newSlackService(t *testing.T, f *fakeSlack) (*Service, *ledger.Repository, string) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	_ = os.Mkdir(repoPath, 0o700)
	runGit(t, repoPath, "init", "-b", "main")
	_ = os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600)
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	t.Setenv("FD_TEST_SLACK_TOKEN", f.token)
	config := credentials.Config{Version: "0.1", Adapters: []credentials.AdapterIdentity{{ID: slackoutbox.AdapterID, Version: slackoutbox.AdapterVersion, TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:" + slackoutbox.AdapterID, Enabled: true}}, Credentials: []credentials.Binding{{ID: "slack-main", Provider: "slack", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_TEST_SLACK_TOKEN"}, AllowedAdapters: []string{slackoutbox.AdapterID}, AllowedOperations: []string{slackoutbox.StatusOperation, slackoutbox.CommitOperation}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "slack.com", PathPrefix: "/api"}}, Enabled: true}}}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, store, store)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}, Credentials: broker, Slack: &slackoutbox.Adapter{HTTPClient: &http.Client{Transport: f}}, CoordinatorID: "test"}, store, repoPath
}

func TestSlackOutboxAmbiguousRecoveryPostsOnce(t *testing.T) {
	f := &fakeSlack{token: "secret", ambiguous: true}
	svc, store, repo := newSlackService(t, f)
	created, err := svc.Create(CreateRequest{Repository: repo, Mode: "cooperative", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600)
	effect, err := svc.PrepareSlackMessage(created.Transaction.ID, PrepareSlackMessageRequest{CredentialID: "slack-main", Input: slackoutbox.Input{Channel: "C12345678", Text: "FutureDiff transaction ready"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "r", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err = svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	mat, _ := svc.ApprovalMaterial(created.Transaction.ID)
	d := mat["transaction_digest"]
	_, _ = svc.Approve(created.Transaction.ID, d, "u")
	if _, err = svc.CommitContext(context.Background(), created.Transaction.ID, d); err == nil {
		t.Fatal("expected ambiguous error")
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("status=%s", stored.Status)
	}
	view, err := svc.Recover(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Transaction.Status != domain.StateCommitted || f.posts != 1 {
		t.Fatalf("status=%s posts=%d", view.Transaction.Status, f.posts)
	}
}
