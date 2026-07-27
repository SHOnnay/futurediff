package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
)

type StaleGitHubBaseReport struct {
	DirectPRCreated       bool
	FutureDiffBlocked     bool
	DirectCreateCalls     int
	FutureDiffCreateCalls int
	CurrentBaseSHA        string
}

func CompareStaleGitHubBase(ctx context.Context) (*StaleGitHubBaseReport, error) {
	report := &StaleGitHubBaseReport{}
	currentSHA := "sha_current"

	var directCreates int
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			directCreates++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 11, "html_url": "https://github.example/acme/payments/pull/11"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer directServer.Close()
	directClient := prcreate.Client{BaseURL: directServer.URL}
	directPrepared := directClient.Prepare(prcreate.CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "Direct stale base benchmark",
		Head:     "agent/direct-stale-base",
		Base:     "main",
		Body:     "Prepared by direct baseline",
		EffectID: "eff_direct_stale",
	})
	if _, err := directClient.Create(ctx, directPrepared); err != nil {
		return nil, fmt.Errorf("direct create pr: %w", err)
	}
	report.DirectPRCreated = true
	report.DirectCreateCalls = directCreates

	var mu sync.Mutex
	futureCreates := 0
	futureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": currentSHA}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			futureCreates++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 22, "html_url": "https://github.example/acme/payments/pull/22"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer futureServer.Close()
	futureClient := prcreate.Client{BaseURL: futureServer.URL}
	futurePrepared := futureClient.Prepare(prcreate.CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "FutureDiff stale base benchmark",
		Head:     "agent/futurediff-stale-base",
		Base:     "main",
		BaseSHA:  "sha_old",
		Body:     "Prepared by FutureDiff",
		EffectID: "eff_future_stale",
	})
	freshness, err := futureClient.CheckBaseFreshness(ctx, futurePrepared)
	if err != nil {
		return nil, fmt.Errorf("check base freshness: %w", err)
	}
	report.CurrentBaseSHA = freshness.CurrentSHA
	if !freshness.Fresh {
		report.FutureDiffBlocked = true
	}
	mu.Lock()
	report.FutureDiffCreateCalls = futureCreates
	mu.Unlock()
	return report, nil
}
