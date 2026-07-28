package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/capability"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/peerauth"
)

func TestAuthorizationGuardRoleAndCapability(t *testing.T) {
	repo, err := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	authorizer, err := authorization.Compile(authorization.Policy{Version: authorization.Version, Default: "deny", AgentRoles: []string{"agent"}, Roles: []authorization.Role{{Name: "agent", Operations: []string{"health"}}}, Bindings: []authorization.Binding{{UID: 1000, Roles: []string{"agent"}}}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	priv, pub, err := operatorapproval.Generate("operator", now)
	if err != nil {
		t.Fatal(err)
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	s := &Server{Service: &app.Service{Ledger: repo}, Authorizer: authorizer, CapabilityKeyring: &ring}
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(http.StatusNoContent) })
	h := s.authorizationGuard(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("role allow failed: %d %d", w.Code, called)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/transactions/tx-1/commit", nil)
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected role denial, got %d", w.Code)
	}

	token, err := capability.Sign(priv, 1000, "transaction_commit", "tx-1", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	compact, _ := capability.EncodeCompact(token)
	req = httptest.NewRequest(http.MethodPost, "/v1/transactions/tx-1/commit", nil)
	req.Header.Set(capabilityHeader, compact)
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || called != 2 {
		t.Fatalf("capability allow failed: %d %d", w.Code, called)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/transactions/tx-1/commit", nil)
	req.Header.Set(capabilityHeader, compact)
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: 1000, GID: 1000, PID: 1, Available: true}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected capability replay denial, got %d", w.Code)
	}

	summary, err := repo.AuthorizationSummary(10)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 4 || summary.Allowed != 2 || summary.Denied != 2 || !summary.ChainValid {
		t.Fatalf("unexpected audit summary: %+v", summary)
	}
}
