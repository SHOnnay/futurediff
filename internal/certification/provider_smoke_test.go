package certification

import (
	"archive/zip"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProviderSmokeAndFuturepack(t *testing.T) {
	githubServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/payments/branches/main":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"sha": "sha_current"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()

	slackServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat.postMessage":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C123456789", "ts": "1712345.920001", "message": map[string]any{"client_msg_id": "provider-smoke"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/conversations.history":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{{
					"ts":            "1712345.920001",
					"client_msg_id": "provider-smoke",
					"metadata": map[string]any{
						"event_type":    "futurediff_effect",
						"event_payload": map[string]any{"effect_id": "provider-smoke-slack"},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer slackServer.Close()

	client := githubServer.Client()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	slackClient := slackServer.Client()
	slackClient.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	futurepackPath := filepath.Join(t.TempDir(), "provider-smoke.futurepack")
	report, err := RunProviderSmoke(context.Background(), ProviderSmokeConfig{
		GitHubBaseURL:     githubServer.URL,
		GitHubHTTPClient:  client,
		GitHubToken:       "github-token",
		GitHubOwner:       "acme",
		GitHubRepo:        "payments",
		GitHubBaseBranch:  "main",
		GitHubExpectedSHA: "sha_old",
		SlackBaseURL:      slackServer.URL + "/api",
		SlackHTTPClient:   slackClient,
		SlackToken:        "slack-token",
		SlackChannel:      "C123456789",
		SlackText:         "Provider smoke benchmark",
	}, ProviderOptions{Root: t.TempDir(), FuturepackPath: futurepackPath})
	if err != nil {
		t.Fatalf("run provider smoke: %v", err)
	}
	if !report.Certified || report.ReportDigest == "" {
		t.Fatalf("unexpected provider report: %+v", report)
	}
	if report.GitHubCurrentBaseSHA != "sha_current" {
		t.Fatalf("unexpected github current base sha: %s", report.GitHubCurrentBaseSHA)
	}
	if report.SlackTimestamp != "1712345.920001" {
		t.Fatalf("unexpected slack timestamp: %s", report.SlackTimestamp)
	}
	if _, err := os.Stat(futurepackPath); err != nil {
		t.Fatalf("expected futurepack output: %v", err)
	}

	archive, err := zip.OpenReader(futurepackPath)
	if err != nil {
		t.Fatalf("open futurepack: %v", err)
	}
	defer archive.Close()
	entries := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = content
	}
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		t.Fatal("expected manifest.json entry")
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["scenario"] != "provider_smoke" {
		t.Fatalf("unexpected manifest scenario: %#v", manifest["scenario"])
	}
	joined := strings.Join(mapKeys(entries), "\n")
	if !strings.Contains(joined, "artifacts/sha256/") {
		t.Fatalf("expected content-addressed artifact entry, entries=%s", joined)
	}
}

func TestProviderSmokeValidation(t *testing.T) {
	_, err := RunProviderSmoke(context.Background(), ProviderSmokeConfig{}, ProviderOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
