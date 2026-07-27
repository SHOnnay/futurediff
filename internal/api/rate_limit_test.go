package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/ratelimit"
)

func TestRateGuardRejectsBurst(t *testing.T) {
	limiter, err := ratelimit.New(ratelimit.Policy{Version: ratelimit.Version, ReadRequestsPerMinute: 1, ReadBurst: 1, MutationRequestsPerMinute: 1, MutationBurst: 1, MaxConcurrentMutations: 1})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{RateLimiter: limiter}
	h := s.rateGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req = req.WithContext(peerauth.WithIdentity(req.Context(), peerauth.Identity{UID: 7}))
	first := httptest.NewRecorder()
	h.ServeHTTP(first, req)
	if first.Code != 204 {
		t.Fatalf("first=%d", first.Code)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("second=%d headers=%v", second.Code, second.Header())
	}
}
