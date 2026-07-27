package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
)

type DuplicateAPIRetryReport struct {
	DirectPullCount     int
	FutureDiffPullCount int
	FutureDiffRecovered bool
}

func CompareDuplicateAPIRetry(ctx context.Context) (*DuplicateAPIRetryReport, error) {
	report := &DuplicateAPIRetryReport{}

	directPulls, directServer := newAmbiguousGitHubServer()
	defer directServer.Close()
	directClient := prcreate.Client{BaseURL: directServer.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}}
	directPrepared := directClient.Prepare(prcreate.CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "Direct retry duplicate benchmark",
		Head:     "agent/direct-duplicate",
		Base:     "main",
		Body:     "Prepared by direct baseline",
		EffectID: "eff_direct_duplicate",
	})
	_, _ = directClient.Create(ctx, directPrepared)
	_, _ = directClient.Create(ctx, directPrepared)
	report.DirectPullCount = len(directPulls.snapshot())

	futurePulls, futureServer := newAmbiguousGitHubServer()
	defer futureServer.Close()
	futureClient := prcreate.Client{BaseURL: futureServer.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}}
	futurePrepared := futureClient.Prepare(prcreate.CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "FutureDiff retry benchmark",
		Head:     "agent/futurediff-duplicate",
		Base:     "main",
		Body:     "Prepared by FutureDiff",
		EffectID: "eff_futurediff_duplicate",
	})
	if _, err := futureClient.Create(ctx, futurePrepared); err == nil {
		return nil, fmt.Errorf("expected ambiguous create timeout")
	}
	receipt, err := futureClient.Recover(ctx, futurePrepared)
	if err != nil {
		return nil, fmt.Errorf("recover future diff create: %w", err)
	}
	report.FutureDiffRecovered = receipt.Recovered
	report.FutureDiffPullCount = len(futurePulls.snapshot())
	return report, nil
}

type pullStore struct {
	mu    sync.Mutex
	pulls []map[string]any
}

func (p *pullStore) add(payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := map[string]any{
		"number":   len(p.pulls) + 1,
		"html_url": fmt.Sprintf("https://github.example/acme/payments/pull/%d", len(p.pulls)+1),
		"title":    payload["title"],
		"body":     payload["body"],
		"head":     map[string]any{"ref": payload["head"]},
		"base":     map[string]any{"ref": payload["base"]},
	}
	p.pulls = append(p.pulls, entry)
}

func (p *pullStore) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]map[string]any, len(p.pulls))
	copy(out, p.pulls)
	return out
}

func newAmbiguousGitHubServer() (*pullStore, *httptest.Server) {
	store := &pullStore{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			store.add(payload)
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(store.snapshot()[len(store.snapshot())-1])
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/pulls":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(store.snapshot())
		default:
			http.NotFound(w, r)
		}
	}))
	return store, server
}
