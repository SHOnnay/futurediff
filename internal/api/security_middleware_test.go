package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/peerauth"
)

func TestPeerGuard(t *testing.T) {
	s := &Server{RequirePeerCredentials: true, AllowedPeerUIDs: map[uint32]struct{}{1000: {}}}
	h := s.peerGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing peer status=%d", rec.Code)
	}
	req = req.WithContext(peerauth.WithIdentity(context.Background(), peerauth.Identity{UID: 1000, Available: true}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("authorized peer status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIdempotencyReplayAndConflict(t *testing.T) {
	repo, err := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	s := &Server{Service: &app.Service{Ledger: repo}}
	calls := 0
	h := s.idempotencyGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, 201, map[string]any{"call": calls})
	}))
	makeReq := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/transactions", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "request-0001")
		return req.WithContext(peerauth.WithIdentity(context.Background(), peerauth.Identity{UID: 1000, Available: true}))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, makeReq(`{"x":1}`))
	if rec.Code != 201 || calls != 1 {
		t.Fatalf("first: code=%d calls=%d body=%s", rec.Code, calls, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, makeReq(`{"x":1}`))
	if rec.Code != 201 || calls != 1 || rec.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay: code=%d calls=%d header=%q", rec.Code, calls, rec.Header().Get("Idempotency-Replayed"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, makeReq(`{"x":2}`))
	if rec.Code != http.StatusConflict || calls != 1 {
		t.Fatalf("conflict: code=%d calls=%d", rec.Code, calls)
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}{"b":2}`))
	var out map[string]any
	if err := decode(req, &out); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}
