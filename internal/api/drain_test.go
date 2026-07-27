package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/drain"
)

func TestDrainRejectsMutationsAndKeepsHealth(t *testing.T) {
	d := drain.New()
	d.Start("test", time.Now())
	s := &Server{Service: &app.Service{}, Drain: d}
	post := httptest.NewRequest(http.MethodPost, "/v1/transactions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, post)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, get)
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"draining":true`) {
		t.Fatal("health missing drain status")
	}
}
