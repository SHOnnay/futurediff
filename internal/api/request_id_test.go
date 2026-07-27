package api

import (
	"github.com/SHOnnay/futurediff/internal/requestid"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDGuard(t *testing.T) {
	s := &Server{}
	h := s.requestIDGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestid.From(r.Context()) == "" {
			t.Fatal("request id missing from context")
		}
		w.WriteHeader(204)
	}))
	req := httptest.NewRequest("GET", "http://local/", nil)
	req.Header.Set("X-Request-ID", "client-request-123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Request-ID") != "client-request-123" {
		t.Fatalf("header=%q", rr.Header().Get("X-Request-ID"))
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("no-store missing")
	}
}
