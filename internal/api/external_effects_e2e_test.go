package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type apiGitHubTransport struct {
	mu        sync.Mutex
	refs      map[string]string
	pulls     []map[string]any
	postCalls int
	token     string
}

func (f *apiGitHubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.Header.Get("Authorization") != "Bearer "+f.token {
		return apiGitHubResponse(401, `{"message":"unauthorized"}`), nil
	}
	if req.Method == http.MethodGet && strings.Contains(req.URL.EscapedPath(), "/git/ref/heads/") {
		encoded := req.URL.EscapedPath()[strings.Index(req.URL.EscapedPath(), "/git/ref/heads/")+len("/git/ref/heads/"):]
		branch, _ := url.PathUnescape(encoded)
		return apiGitHubResponse(200, `{"object":{"sha":"`+f.refs[branch]+`"}}`), nil
	}
	if req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/pulls") {
		b, _ := json.Marshal(f.pulls)
		return apiGitHubResponse(200, string(b)), nil
	}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/pulls") {
		f.postCalls++
		var p struct {
			Title string `json:"title"`
			Head  string `json:"head"`
			Base  string `json:"base"`
			Body  string `json:"body"`
			Draft bool   `json:"draft"`
		}
		_ = json.NewDecoder(req.Body).Decode(&p)
		created := map[string]any{"number": 901, "node_id": "node-901", "html_url": "https://github.com/acme/app/pull/901", "title": p.Title, "body": p.Body, "draft": p.Draft, "head": map[string]any{"ref": p.Head, "sha": f.refs[p.Head]}, "base": map[string]any{"ref": p.Base, "sha": f.refs[p.Base]}}
		f.pulls = append(f.pulls, created)
		b, _ := json.Marshal(created)
		return apiGitHubResponse(201, string(b)), nil
	}
	return apiGitHubResponse(404, `{"message":"unexpected"}`), nil
}

func apiGitHubResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestUnixSocketGitHubDraftPREffectLifecycle(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "add", ".")
	gitCmd(t, repoPath, "commit", "-m", "base")

	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fake := &apiGitHubTransport{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "socket-token"}
	t.Setenv("FD_API_GITHUB_TOKEN", fake.token)
	config := credentials.Config{Version: "0.1", Adapters: []credentials.AdapterIdentity{{ID: githubdraft.AdapterID, Version: githubdraft.AdapterVersion, TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin", Enabled: true}}, Credentials: []credentials.Binding{{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_API_GITHUB_TOKEN"}, AllowedAdapters: []string{githubdraft.AdapterID}, AllowedOperations: []string{githubdraft.ReadOperation, githubdraft.StatusOperation, githubdraft.CommitOperation}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos/acme/app"}}, Enabled: true}}}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, store, store)
	if err != nil {
		t.Fatal(err)
	}
	svc := &app.Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}, Credentials: broker, GitHub: &githubdraft.Adapter{HTTPClient: &http.Client{Transport: fake}}, CoordinatorID: "api-test"}
	socket := shortSocketPath(t, "fd-api-github-")
	server := &Server{Service: svc, SocketPath: socket}
	go func() { _ = server.Serve() }()
	defer server.Close()
	client := NewClient(socket)
	for i := 0; i < 100; i++ {
		if _, err := client.Do("GET", "/v1/health", nil); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	createdRaw, err := client.Do("POST", "/v1/transactions", app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var created app.TransactionView
	if err := json.Unmarshal(createdRaw, &created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := created.Transaction.ID
	prepare := app.PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Title: "Socket PR", Head: "feature/futurediff", Base: "main"}}
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/effects/github/draft-pull-request", prepare); err != nil {
		t.Fatal(err)
	}
	effectsRaw, err := client.Do("GET", "/v1/transactions/"+id+"/effects", nil)
	if err != nil {
		t.Fatal(err)
	}
	var effects []domain.ExternalEffect
	if err := json.Unmarshal(effectsRaw, &effects); err != nil || len(effects) != 1 {
		t.Fatalf("effects=%s err=%v", effectsRaw, err)
	}
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/seal", nil); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "c", PolicyVersion: "p", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/verify", contract); err != nil {
		t.Fatal(err)
	}
	materialRaw, err := client.Do("GET", "/v1/transactions/"+id+"/approval-material", nil)
	if err != nil {
		t.Fatal(err)
	}
	var material map[string]string
	_ = json.Unmarshal(materialRaw, &material)
	if _, err := client.Do("POST", "/v1/transactions/"+id+"/approve", map[string]string{"transaction_digest": material["transaction_digest"], "approver": "api-test"}); err != nil {
		t.Fatal(err)
	}
	resultRaw, err := client.Do("POST", "/v1/transactions/"+id+"/commit", map[string]string{"transaction_digest": material["transaction_digest"]})
	if err != nil {
		t.Fatal(err)
	}
	var result app.TransactionView
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Transaction.Status != domain.StateCommitted || len(result.Receipts) != 1 || fake.postCalls != 1 {
		t.Fatalf("result=%s posts=%d", resultRaw, fake.postCalls)
	}
}
