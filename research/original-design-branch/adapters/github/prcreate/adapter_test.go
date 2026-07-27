package prcreate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecoverAfterAmbiguousCreateTimeout(t *testing.T) {
	var (
		mu        sync.Mutex
		pulls     []pull
		postCalls int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			postCalls++
			var payload struct {
				Title string `json:"title"`
				Head  string `json:"head"`
				Base  string `json:"base"`
				Body  string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			pulls = append(pulls, pull{
				Number:  431,
				HTMLURL: "https://github.example/acme/payments/pull/431",
				Title:   payload.Title,
				Body:    payload.Body,
				Head: struct {
					Ref string `json:"ref"`
				}{Ref: payload.Head},
				Base: struct {
					Ref string `json:"ref"`
				}{Ref: payload.Base},
			})
			mu.Unlock()
			time.Sleep(250 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pulls[len(pulls)-1])
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/pulls":
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(pulls)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL, HTTPClient: &http.Client{Timeout: 100 * time.Millisecond}}
	prepared := client.Prepare(CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "Add customer_status migration",
		Head:     "agent/customer-status",
		Base:     "main",
		Body:     "Prepared by FutureDiff",
		EffectID: "eff_123",
	})

	if _, err := client.Create(context.Background(), prepared); err == nil {
		t.Fatal("expected timeout from ambiguous create request")
	}

	receipt, err := client.Recover(context.Background(), prepared)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !receipt.Recovered {
		t.Fatal("expected recovered receipt")
	}
	if receipt.PullNumber != 431 {
		t.Fatalf("unexpected pull number: %d", receipt.PullNumber)
	}

	mu.Lock()
	defer mu.Unlock()
	if postCalls != 1 {
		t.Fatalf("expected one create call, got %d", postCalls)
	}
	if len(pulls) != 1 {
		t.Fatalf("expected one stored pull request, got %d", len(pulls))
	}
}

func TestPrepareCarriesPreviewSupportLevel(t *testing.T) {
	client := Client{}
	prepared := client.Prepare(CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "Test PR",
		Head:     "feature/test-pr",
		Base:     "main",
		Body:     "Prepared by FutureDiff",
		EffectID: "eff_456",
	})

	if prepared.SupportLevel != SupportLevelPreviewWithFreshnessCheck {
		t.Fatalf("unexpected support level: %s", prepared.SupportLevel)
	}
	if prepared.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if !strings.Contains(prepared.PreviewBody, "FutureDiff-Effect: eff_456") {
		t.Fatalf("expected preview body marker, got %q", prepared.PreviewBody)
	}
}

func TestCheckBaseFreshnessDetectsStaleBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commit": map[string]any{"sha": "sha_new"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL}
	prepared := client.Prepare(CreateRequest{
		Owner:    "acme",
		Repo:     "payments",
		Title:    "Detect stale base",
		Head:     "agent/stale-base",
		Base:     "main",
		BaseSHA:  "sha_old",
		Body:     "Prepared by FutureDiff",
		EffectID: "eff_stale",
	})

	result, err := client.CheckBaseFreshness(context.Background(), prepared)
	if err != nil {
		t.Fatalf("check freshness: %v", err)
	}
	if result.Fresh {
		t.Fatal("expected stale base to be detected")
	}
	if result.CurrentSHA != "sha_new" {
		t.Fatalf("unexpected current sha: %s", result.CurrentSHA)
	}
}

func TestCheckBaseFreshnessPassesWithoutExpectedSHA(t *testing.T) {
	client := Client{}
	prepared := client.Prepare(CreateRequest{Owner: "acme", Repo: "payments", Title: "No SHA", Head: "agent/no-sha", Base: "main"})
	result, err := client.CheckBaseFreshness(context.Background(), prepared)
	if err != nil {
		t.Fatalf("check freshness without expected sha: %v", err)
	}
	if !result.Fresh {
		t.Fatal("expected freshness to pass when no base sha is pinned")
	}
}

func TestCloseCompensatesCommittedPullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/payments/pulls/431":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   431,
				"html_url": "https://github.example/acme/payments/pull/431",
				"state":    "closed",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{BaseURL: server.URL}
	prepared := client.Prepare(CreateRequest{Owner: "acme", Repo: "payments", Title: "Compensate PR", Head: "agent/compensate-pr", Base: "main", EffectID: "eff_compensate"})
	receipt, err := client.Close(context.Background(), prepared, &Receipt{PullNumber: 431, HTMLURL: "https://github.example/acme/payments/pull/431"})
	if err != nil {
		t.Fatalf("close compensation: %v", err)
	}
	if receipt.State != "closed" {
		t.Fatalf("expected closed state, got %s", receipt.State)
	}
	if receipt.PullNumber != 431 {
		t.Fatalf("unexpected pull number: %d", receipt.PullNumber)
	}
}
