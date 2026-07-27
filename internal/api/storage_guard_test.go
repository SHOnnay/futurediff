package api

import (
	"github.com/SHOnnay/futurediff/internal/storageguard"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type storageProbe struct{}

func (storageProbe) Inspect(string, string, string) (storageguard.Filesystem, int64, int64, error) {
	return storageguard.Filesystem{TotalBytes: 100, FreeBytes: 1, FreePercent: 1}, 0, 0, nil
}
func TestStorageGuardBlocksMutations(t *testing.T) {
	s := &Server{StorageGuard: &storageguard.Guard{Root: t.TempDir(), Policy: storageguard.Policy{Version: storageguard.Version, MinimumFreeBytes: 10}, Probe: storageProbe{}, CacheTTL: time.Hour}}
	h := s.storageGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/transactions", nil))
	if w.Code != http.StatusInsufficientStorage {
		t.Fatalf("status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if w.Code != 204 {
		t.Fatalf("get status=%d", w.Code)
	}
}
