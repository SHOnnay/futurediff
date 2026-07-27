package api

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func TestHealthExposesOnlyCredentialCountsNotSecretMaterial(t *testing.T) {
	repo, err := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	t.Setenv("FD_HEALTH_SECRET", "do-not-leak-this-value")
	config := credentials.Config{Version: "0.1", Adapters: []credentials.AdapterIdentity{{ID: "github", Version: "0.1", TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:github@0.1", Enabled: true}}, Credentials: []credentials.Binding{{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_HEALTH_SECRET"}, AllowedAdapters: []string{"github"}, AllowedOperations: []string{"github.create_draft_pull_request"}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos"}}, Enabled: true}}}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	service := &app.Service{Ledger: repo, Staging: staging.Manager{RuntimeRoot: t.TempDir()}, Verifier: verification.Engine{}, Credentials: broker}
	server := &Server{Service: service}
	request := httptest.NewRequest("GET", "/v1/health", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "do-not-leak-this-value") || strings.Contains(body, "FD_HEALTH_SECRET") {
		t.Fatalf("health response leaked secret metadata: %s", body)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	credentialStatus, ok := decoded["credentials"].(map[string]any)
	if !ok || credentialStatus["configured"] != true || credentialStatus["secret_values_persisted"] != false {
		t.Fatalf("unexpected status: %#v", decoded)
	}
}
