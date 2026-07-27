package credentials_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

func TestEnvironmentCredentialUseIsDurablyAudited(t *testing.T) {
	repo, err := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	t.Setenv("FD_INTEGRATION_TOKEN", "integration-secret")
	config := credentials.Config{Version: "0.1", Adapters: []credentials.AdapterIdentity{{ID: "github", Version: "0.1", TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:github@0.1", Enabled: true}}, Credentials: []credentials.Binding{{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "FD_INTEGRATION_TOKEN"}, AllowedAdapters: []string{"github"}, AllowedOperations: []string{"github.create_draft_pull_request"}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos"}}, Enabled: true}}}
	broker, err := credentials.NewBroker(config, credentials.EnvironmentSource{}, repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	used := false
	err = broker.WithCredential(context.Background(), credentials.AccessRequest{TransactionID: "tx-integration", EffectID: "effect-integration", AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/acme/app/pulls"}, func(secret credentials.Secret) error {
		used = string(secret.CopyBytes()) == "integration-secret"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Fatal("secret was not available inside trusted callback")
	}
	events, err := repo.CredentialAccessEvents("github-main")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Decision != credentials.DecisionGranted || events[0].TransactionID != "tx-integration" {
		t.Fatalf("unexpected events: %#v", events)
	}
}
