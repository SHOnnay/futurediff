package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/credentials"
)

func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func TestCredentialMetadataAndAuditNeverPersistSecretValue(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	repo, err := OpenRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	adapter := credentials.AdapterIdentity{ID: "github", Version: "0.1", TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "builtin:github@0.1", Enabled: true}
	binding := credentials.Binding{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "SUPER_SECRET_ENV_NAME"}, AllowedAdapters: []string{"github"}, AllowedOperations: []string{"github.create_draft_pull_request"}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos"}}, Enabled: true}
	if err := repo.RegisterAdapterIdentity(adapter); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegisterCredentialBinding(binding); err != nil {
		t.Fatal(err)
	}
	event := credentials.AccessEvent{EventID: "evt-1", AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/a/b/pulls", Decision: credentials.DecisionGranted, Reason: "scope validated", CreatedAt: time.Now().UTC()}
	if err := repo.RecordCredentialAccess(event); err != nil {
		t.Fatal(err)
	}
	raw, err := osReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SUPER_SECRET_ENV_NAME") {
		t.Fatal("secret source reference persisted in plaintext")
	}
	if strings.Contains(string(raw), "actual-secret-value") {
		t.Fatal("secret value persisted")
	}
	events, err := repo.CredentialAccessEvents("github-main")
	if err != nil || len(events) != 1 || events[0].Decision != credentials.DecisionGranted {
		t.Fatalf("audit events: %#v %v", events, err)
	}
	counts, err := repo.CredentialMetadataCounts()
	if err != nil || counts["enabled_adapters"] != 1 || counts["enabled_credentials"] != 1 || counts["access_events"] != 1 {
		t.Fatalf("counts: %#v %v", counts, err)
	}
}

func TestAdapterIdentityCannotSilentlyChangeTrustOrDigest(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	first := credentials.AdapterIdentity{ID: "github", Version: "0.1", TrustLevel: credentials.TrustBuiltIn, ExecutableDigest: "digest-a", Enabled: true}
	if err := repo.RegisterAdapterIdentity(first); err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.ExecutableDigest = "digest-b"
	if err := repo.RegisterAdapterIdentity(changed); err == nil {
		t.Fatal("expected digest change rejection")
	}
	changed = first
	changed.TrustLevel = credentials.TrustVerified
	if err := repo.RegisterAdapterIdentity(changed); err == nil {
		t.Fatal("expected trust change rejection")
	}
}

func TestCredentialBindingCannotSilentlyChangeSourceIdentity(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	binding := credentials.Binding{ID: "github-main", Provider: "github", Source: credentials.SecretSourceRef{Kind: "environment", Reference: "TOKEN_A"}, AllowedAdapters: []string{"github"}, AllowedOperations: []string{"github.create"}, AllowedDestinations: []credentials.DestinationRule{{Scheme: "https", Host: "api.github.com"}}, Enabled: true}
	if err := repo.RegisterCredentialBinding(binding); err != nil {
		t.Fatal(err)
	}
	binding.Source.Reference = "TOKEN_B"
	if err := repo.RegisterCredentialBinding(binding); err == nil {
		t.Fatal("expected source identity change rejection")
	}
}
