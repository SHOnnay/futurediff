package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type appFakeGitHub struct {
	mu            sync.Mutex
	refs          map[string]string
	pulls         []map[string]any
	postCalls     int
	postMode      string
	token         string
	lastAuth      string
	lastPostDraft bool
}

func (f *appFakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastAuth = req.Header.Get("Authorization")
	if f.lastAuth != "Bearer "+f.token {
		return appResponse(401, `{"message":"unauthorized"}`), nil
	}
	pathValue := req.URL.EscapedPath()
	switch {
	case req.Method == http.MethodGet && strings.Contains(pathValue, "/git/ref/heads/"):
		encoded := pathValue[strings.Index(pathValue, "/git/ref/heads/")+len("/git/ref/heads/"):]
		branch, _ := url.PathUnescape(encoded)
		sha := f.refs[branch]
		if sha == "" {
			return appResponse(404, `{"message":"missing"}`), nil
		}
		return appResponse(200, `{"object":{"sha":"`+sha+`"}}`), nil
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/pulls"):
		b, _ := json.Marshal(f.pulls)
		return appResponse(200, string(b)), nil
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/pulls"):
		f.postCalls++
		var payload struct {
			Title string `json:"title"`
			Head  string `json:"head"`
			Base  string `json:"base"`
			Body  string `json:"body"`
			Draft bool   `json:"draft"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		f.lastPostDraft = payload.Draft
		if f.postMode == "reject" {
			return appResponse(422, `{"message":"validation failed"}`), nil
		}
		pull := map[string]any{
			"number": 431, "node_id": "node-431", "html_url": "https://github.com/acme/app/pull/431",
			"title": payload.Title, "body": payload.Body, "draft": payload.Draft,
			"head": map[string]any{"ref": payload.Head, "sha": f.refs[payload.Head]},
			"base": map[string]any{"ref": payload.Base, "sha": f.refs[payload.Base]},
		}
		f.pulls = append(f.pulls, pull)
		if f.postMode == "ambiguous" {
			return nil, errors.New("connection reset after provider accepted request")
		}
		b, _ := json.Marshal(pull)
		return appResponse(201, string(b)), nil
	default:
		return appResponse(404, `{"message":"unexpected route"}`), nil
	}
}

func appResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func newExternalEffectService(t *testing.T, fake *appFakeGitHub) (*Service, *ledger.Repository, string) {
	t.Helper()
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "base")
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Setenv("FD_TEST_GITHUB_TOKEN", fake.token)
	config := credentials.Config{
		Version:  "0.1",
		Adapters: []credentials.AdapterIdentity{{ID: githubdraft.AdapterID, Version: githubdraft.AdapterVersion, TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:" + githubdraft.AdapterID + "@" + githubdraft.AdapterVersion, Enabled: true}},
		Credentials: []credentials.Binding{{
			ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_TEST_GITHUB_TOKEN"},
			AllowedAdapters: []string{githubdraft.AdapterID}, AllowedOperations: []string{githubdraft.ReadOperation, githubdraft.StatusOperation, githubdraft.CommitOperation},
			AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos/acme/app"}}, Enabled: true,
		}},
	}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, store, store)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{},
		Credentials: broker, GitHub: &githubdraft.Adapter{HTTPClient: &http.Client{Transport: fake}}, CoordinatorID: "test-coordinator",
	}
	return svc, store, repoPath
}

func prepareReadyTransaction(t *testing.T, svc *Service, repoPath string) (TransactionView, domain.ExternalEffect, string) {
	t.Helper()
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	effect, err := svc.PrepareGitHubDraftPR(context.Background(), created.Transaction.ID, PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Title: "FutureDiff change", Body: "Prepared safely", Head: "feature/futurediff", Base: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "basic", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	ready, err := svc.Verify(created.Transaction.ID, contract)
	if err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(created.Transaction.ID, digest, "test-user"); err != nil {
		t.Fatal(err)
	}
	return ready, effect, digest
}

func TestGitHubDraftPRTransactionCommitsOnce(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "super-secret-token"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, effect, digest := prepareReadyTransaction(t, svc, repoPath)
	committed, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Transaction.Status != domain.StateCommitted {
		t.Fatalf("status=%s", committed.Transaction.Status)
	}
	if len(committed.Effects) != 1 || committed.Effects[0].Status != domain.EffectCommitted || len(committed.Receipts) != 1 {
		t.Fatalf("effects/receipts not committed: %#v %#v", committed.Effects, committed.Receipts)
	}
	if fake.postCalls != 1 || !fake.lastPostDraft {
		t.Fatalf("expected one draft PR POST, calls=%d draft=%t", fake.postCalls, fake.lastPostDraft)
	}
	live, _ := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if string(live) != "current\n" {
		t.Fatalf("live checkout changed: %q", live)
	}
	future := runGit(t, repoPath, "show", "refs/heads/futurediff/"+ready.Transaction.ID+":README.md")
	if future != "future" {
		t.Fatalf("future ref mismatch: %q", future)
	}
	access, err := store.CredentialAccessEvents("github-main")
	if err != nil {
		t.Fatal(err)
	}
	if len(access) < 4 {
		t.Fatalf("expected separate read/create audits, got %d", len(access))
	}
	for _, event := range access {
		if event.Operation == githubdraft.CommitOperation && !strings.HasSuffix(event.Destination, "/pulls") {
			t.Fatalf("mutation grant used wrong destination: %#v", event)
		}
	}
	rows, err := store.Events(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), fake.token) {
		t.Fatal("secret leaked into transaction events")
	}
	stored, err := store.ExternalEffect(effect.EffectID)
	if err != nil || stored.Status != domain.EffectCommitted {
		t.Fatalf("stored effect: %#v %v", stored, err)
	}
}

func TestAmbiguousGitHubCreateRecoversWithoutDuplicate(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret", postMode: "ambiguous"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, effect, digest := prepareReadyTransaction(t, svc, repoPath)
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected ambiguous commit error")
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("status=%s", tx.Status)
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectUnknown {
		t.Fatalf("effect status=%s", stored.Status)
	}
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateCommitted || fake.postCalls != 1 {
		t.Fatalf("recovery=%s posts=%d", recovered.Transaction.Status, fake.postCalls)
	}
}

func TestGitHubRefChangeInvalidatesApprovalBeforeRelease(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, effect, digest := prepareReadyTransaction(t, svc, repoPath)
	fake.mu.Lock()
	fake.refs["main"] = strings.Repeat("c", 40)
	fake.mu.Unlock()
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected stale commit rejection")
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateStale || tx.ApprovalDigest != "" || fake.postCalls != 0 {
		t.Fatalf("stale handling: status=%s approval=%q posts=%d", tx.Status, tx.ApprovalDigest, fake.postCalls)
	}
	refreshed, err := svc.RefreshGitHubEffect(context.Background(), ready.Transaction.ID, effect.EffectID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ResourceVersions["github://acme/app/refs/heads/main"] != strings.Repeat("c", 40) {
		t.Fatalf("resource version not refreshed: %#v", refreshed.ResourceVersions)
	}
}

func TestDefiniteProviderRejectionCanRearm(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret", postMode: "reject"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	ready, effect, digest := prepareReadyTransaction(t, svc, repoPath)
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, digest); err == nil {
		t.Fatal("expected provider rejection")
	}
	tx, _ := store.Get(ready.Transaction.ID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("status=%s", tx.Status)
	}
	recovered, err := svc.Recover(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateReady {
		t.Fatalf("expected ready after provider absence proved, got %s", recovered.Transaction.Status)
	}
	stored, _ := store.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectVerified || fake.postCalls != 1 {
		t.Fatalf("effect=%s posts=%d", stored.Status, fake.postCalls)
	}
}

func TestPreparedEffectIsBoundIntoApprovalAndAbortIsDurable(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	effect, err := svc.PrepareGitHubDraftPR(context.Background(), created.Transaction.ID, PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Title: "FutureDiff change", Head: "feature/futurediff", Base: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Abort(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	tx, _ := store.Get(created.Transaction.ID)
	stored, _ := store.ExternalEffect(effect.EffectID)
	if tx.Status != domain.StateAborted || stored.Status != domain.EffectAborted || fake.postCalls != 0 {
		t.Fatalf("abort state tx=%s effect=%s posts=%d", tx.Status, stored.Status, fake.postCalls)
	}
}

func TestEffectRefreshChangesApprovalDigest(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "secret"}
	svc, _, repoPath := newExternalEffectService(t, fake)
	ready, effect, oldDigest := prepareReadyTransaction(t, svc, repoPath)
	fake.mu.Lock()
	fake.refs["main"] = strings.Repeat("c", 40)
	fake.mu.Unlock()
	if _, err := svc.CommitContext(context.Background(), ready.Transaction.ID, oldDigest); err == nil {
		t.Fatal("expected stale rejection")
	}
	if _, err := svc.RefreshGitHubEffect(context.Background(), ready.Transaction.ID, effect.EffectID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "basic", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := svc.Verify(ready.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(ready.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if material["transaction_digest"] == oldDigest {
		t.Fatal("effect resource-version refresh did not change approval digest")
	}
}
