package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/maintenance"
)

func TestMaintenanceBlocksMutationsButAllowsHealth(t *testing.T) {
	m := &maintenance.Manager{Path: filepath.Join(t.TempDir(), "maintenance.json")}
	if _, err := m.Enable("backup", "test", 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &Server{Service: &app.Service{}, Maintenance: m}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/transactions", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", w.Code, w.Body.String())
	}
}
