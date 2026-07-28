package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/staging"
)

func tenantRequest(t *testing.T, h http.Handler, uid uint32, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: uid, GID: uid, PID: 1, Available: true}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestTenantIsolationListingSharingAndAllScope(t *testing.T) {
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
	authz, err := authorization.Compile(authorization.Policy{Version: authorization.Version, Default: "deny", Roles: []authorization.Role{
		{Name: "tenant", ResourceScope: "owned", Operations: []string{"transaction_create", "transaction_list", "transaction_get", "transaction_seal", "transaction_access_list", "transaction_access_grant", "transaction_access_revoke"}},
		{Name: "operator", ResourceScope: "all", Operations: []string{"*"}},
	}, Bindings: []authorization.Binding{{UID: 1000, Roles: []string{"tenant"}}, {UID: 1001, Roles: []string{"tenant"}}, {UID: 2000, Roles: []string{"operator"}}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Service: &app.Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}}, Authorizer: authz}
	h := s.Handler()
	create := func(uid uint32) app.TransactionView {
		w := tenantRequest(t, h, uid, http.MethodPost, "/v1/transactions", app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "p"})
		if w.Code != http.StatusCreated {
			t.Fatalf("create uid %d: %d %s", uid, w.Code, w.Body.String())
		}
		var v app.TransactionView
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	a := create(1000)
	b := create(1001)
	if a.Transaction.OwnerPrincipalID != "uid:1000" || b.Transaction.OwnerPrincipalID != "uid:1001" {
		t.Fatalf("owners: %q %q", a.Transaction.OwnerPrincipalID, b.Transaction.OwnerPrincipalID)
	}
	w := tenantRequest(t, h, 1000, http.MethodGet, "/v1/transactions", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list["transactions"].([]any)) != 1 {
		t.Fatalf("owner list %s", w.Body.String())
	}
	w = tenantRequest(t, h, 1000, http.MethodGet, "/v1/transactions/"+b.Transaction.ID, nil)
	if w.Code != 404 {
		t.Fatalf("cross-tenant get %d %s", w.Code, w.Body.String())
	}
	w = tenantRequest(t, h, 1000, http.MethodPut, "/v1/transactions/"+a.Transaction.ID+"/access/uid:1001", map[string]string{"permission": "read"})
	if w.Code != 200 {
		t.Fatalf("grant %d %s", w.Code, w.Body.String())
	}
	w = tenantRequest(t, h, 1001, http.MethodGet, "/v1/transactions/"+a.Transaction.ID, nil)
	if w.Code != 200 {
		t.Fatalf("shared get %d %s", w.Code, w.Body.String())
	}
	if err := os.WriteFile(filepath.Join(a.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w = tenantRequest(t, h, 1001, http.MethodPost, "/v1/transactions/"+a.Transaction.ID+"/seal", nil)
	if w.Code != 404 {
		t.Fatalf("read grant must not operate: %d %s", w.Code, w.Body.String())
	}
	w = tenantRequest(t, h, 1000, http.MethodPut, "/v1/transactions/"+a.Transaction.ID+"/access/uid:1001", map[string]string{"permission": "operate"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = tenantRequest(t, h, 1001, http.MethodPost, "/v1/transactions/"+a.Transaction.ID+"/seal", nil)
	if w.Code != 200 {
		t.Fatalf("operate grant seal: %d %s", w.Code, w.Body.String())
	}
	w = tenantRequest(t, h, 1001, http.MethodGet, "/v1/transactions/"+a.Transaction.ID+"/access", nil)
	if w.Code != 404 {
		t.Fatalf("shared principal must not administer: %d", w.Code)
	}
	w = tenantRequest(t, h, 1000, http.MethodDelete, "/v1/transactions/"+a.Transaction.ID+"/access/uid:1001", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = tenantRequest(t, h, 1001, http.MethodGet, "/v1/transactions/"+a.Transaction.ID, nil)
	if w.Code != 404 {
		t.Fatalf("revoked get %d", w.Code)
	}
	w = tenantRequest(t, h, 2000, http.MethodGet, "/v1/transactions", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list["transactions"].([]any)) != 2 {
		t.Fatalf("all-scope list %s", w.Body.String())
	}
	if _, err := store.VerifyTransactionAccessChain(); err != nil {
		t.Fatal(err)
	}
}
