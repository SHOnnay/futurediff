package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func TestOperatorAuditCapturesCreateAndAbortLifecycle(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "add", ".")
	gitCmd(t, repoPath, "commit", "-m", "base")

	repo, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	trail := &operatoraudit.Store{Root: filepath.Join(tmp, "fdroot"), Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	server := &Server{Service: &app.Service{Ledger: repo, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}}, OperatorAudit: trail}
	h := server.Handler()

	createBody, _ := json.Marshal(app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
	createReq := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(createBody))
	createReq = createReq.WithContext(peerauth.WithIdentity(createReq.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created app.TransactionView
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	abortReq := httptest.NewRequest(http.MethodPost, "/v1/transactions/"+created.Transaction.ID+"/abort", nil)
	abortReq = abortReq.WithContext(peerauth.WithIdentity(abortReq.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	abortRec := httptest.NewRecorder()
	h.ServeHTTP(abortRec, abortReq)
	if abortRec.Code != http.StatusOK {
		t.Fatalf("abort status=%d body=%s", abortRec.Code, abortRec.Body.String())
	}

	report, err := trail.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 4 {
		t.Fatalf("unexpected report: %+v", report)
	}
	events, err := trail.Events()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{events[0].EventType, events[1].EventType, events[2].EventType, events[3].EventType}
	want := []string{"transaction.create.request", "transaction.create.result", "transaction.abort.request", "transaction.abort.result"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event order=%v", got)
	}
	for _, event := range events {
		for _, value := range event.Metadata {
			if strings.Contains(value, repoPath) {
				t.Fatalf("repository path leaked into audit trail: %q", value)
			}
		}
	}
	if repoDigest := events[0].Metadata["repository"]; !strings.HasPrefix(repoDigest, "repo:sha256:") {
		t.Fatalf("repository digest=%q", repoDigest)
	}
}

func TestOperatorAuditFailsClosedBeforeMutation(t *testing.T) {
	tmp := t.TempDir()
	repoPath := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repoPath, "add", ".")
	gitCmd(t, repoPath, "commit", "-m", "base")

	repo, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	trail := &operatoraudit.Store{Root: filepath.Join(tmp, "fdroot")}
	if err := os.MkdirAll(filepath.Dir(trail.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trail.Path(), []byte("{\"broken\""), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{Service: &app.Service{Ledger: repo, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}, Verifier: verification.Engine{}}, OperatorAudit: trail}
	h := server.Handler()

	createBody, _ := json.Marshal(app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
	createReq := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(createBody))
	createReq = createReq.WithContext(peerauth.WithIdentity(createReq.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	transactions, err := repo.ListTransactionsForPrincipal("uid:1000", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 0 {
		t.Fatalf("unexpected transaction count=%d", len(transactions))
	}
}

func TestOperatorAuditRecordsAuthorizationDenial(t *testing.T) {
	repo, err := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	authorizer, err := authorization.Compile(authorization.Policy{Version: authorization.Version, Default: "deny", AgentRoles: []string{"agent"}, Roles: []authorization.Role{{Name: "agent", Operations: []string{"health"}}}, Bindings: []authorization.Binding{{UID: 1000, Roles: []string{"agent"}}}})
	if err != nil {
		t.Fatal(err)
	}
	trail := &operatoraudit.Store{Root: filepath.Join(t.TempDir(), "fdroot")}
	server := &Server{Service: &app.Service{Ledger: repo}, Authorizer: authorizer, OperatorAudit: trail}
	called := 0
	h := server.requestIDGuard(server.authorizationGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(http.StatusNoContent) })))
	request := httptest.NewRequest(http.MethodPost, "/v1/transactions/tx-1/commit", nil)
	request = request.WithContext(peerauth.WithIdentity(request.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called != 0 {
		t.Fatalf("status=%d called=%d body=%s", response.Code, called, response.Body.String())
	}
	events, err := trail.Events()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "authorization.denied" || events[0].Result != operatoraudit.ResultDenied {
		t.Fatalf("unexpected events: %+v", events)
	}
	if got := events[0].Metadata["reason"]; got == "" {
		t.Fatal("missing denial reason metadata")
	}
}
