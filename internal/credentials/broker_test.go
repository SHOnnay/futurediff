package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type memoryAudit struct {
	events []AccessEvent
	err    error
}

func (m *memoryAudit) RecordCredentialAccess(e AccessEvent) error {
	m.events = append(m.events, e)
	return m.err
}

type memoryMetadata struct {
	adapters []AdapterIdentity
	bindings []Binding
}

func (m *memoryMetadata) RegisterAdapterIdentity(a AdapterIdentity) error {
	m.adapters = append(m.adapters, a)
	return nil
}
func (m *memoryMetadata) RegisterCredentialBinding(b Binding) error {
	m.bindings = append(m.bindings, b)
	return nil
}

type fixedSource struct {
	value string
	err   error
}

func (f fixedSource) Resolve(context.Context, SecretSourceRef) (Secret, error) {
	if f.err != nil {
		return Secret{}, f.err
	}
	return newSecret(f.value), nil
}

func testConfig() Config {
	return Config{Version: "0.1", Adapters: []AdapterIdentity{{ID: "github", Version: "0.1", TrustLevel: TrustBuiltIn, ExecutableDigest: "builtin:github@0.1", Enabled: true}, {ID: "unknown", Version: "0.1", TrustLevel: TrustUntrusted, Enabled: true}}, Credentials: []Binding{{ID: "github-main", Provider: "github", Source: SecretSourceRef{Kind: "environment", Reference: "GITHUB_TOKEN"}, AllowedAdapters: []string{"github"}, AllowedOperations: []string{"github.create_draft_pull_request"}, AllowedDestinations: []DestinationRule{{Scheme: "https", Host: "api.github.com", PathPrefix: "/repos"}}, Enabled: true}}}
}

func TestBrokerGrantsScopedTrustedAccessAndRedactsSecret(t *testing.T) {
	audit := &memoryAudit{}
	metadata := &memoryMetadata{}
	broker, err := NewBroker(testConfig(), fixedSource{value: "top-secret-token"}, audit, metadata)
	if err != nil {
		t.Fatal(err)
	}
	var received []byte
	err = broker.WithCredential(context.Background(), AccessRequest{TransactionID: "tx1", EffectID: "effect1", AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/acme/app/pulls"}, func(secret Secret) error {
		received = secret.CopyBytes()
		if secret.String() != "[REDACTED]" {
			t.Fatalf("secret String leaked")
		}
		encoded, _ := json.Marshal(secret)
		if strings.Contains(string(encoded), "top-secret") {
			t.Fatalf("secret JSON leaked")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != "top-secret-token" {
		t.Fatalf("adapter did not receive secret")
	}
	if len(audit.events) != 1 || audit.events[0].Decision != DecisionGranted {
		t.Fatalf("expected granted audit event: %#v", audit.events)
	}
	raw, _ := json.Marshal(audit.events[0])
	if strings.Contains(string(raw), "top-secret-token") {
		t.Fatalf("audit leaked secret")
	}
	if len(metadata.adapters) != 2 || len(metadata.bindings) != 1 {
		t.Fatalf("metadata not registered")
	}
}

func TestBrokerDeniesUntrustedAdapterWrongOperationAndDestination(t *testing.T) {
	cases := []AccessRequest{
		{AdapterID: "unknown", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/a/b/pulls"},
		{AdapterID: "github", CredentialID: "github-main", Operation: "github.merge_pull_request", Destination: "https://api.github.com/repos/a/b/pulls"},
		{AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://evil.example/repos/a/b/pulls"},
		{AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos-evil/a/b"},
	}
	for _, req := range cases {
		audit := &memoryAudit{}
		broker, err := NewBroker(testConfig(), fixedSource{value: "secret"}, audit, nil)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		if err := broker.WithCredential(context.Background(), req, func(Secret) error { called = true; return nil }); err == nil {
			t.Fatalf("expected denial for %#v", req)
		}
		if called {
			t.Fatalf("consumer called for denied request")
		}
		if len(audit.events) != 1 || audit.events[0].Decision != DecisionDenied {
			t.Fatalf("missing denial audit")
		}
	}
}

func TestBrokerDeniesExpiredBindingAndFailsClosedOnAuditError(t *testing.T) {
	config := testConfig()
	expired := time.Now().Add(-time.Minute)
	config.Credentials[0].ExpiresAt = &expired
	audit := &memoryAudit{}
	broker, _ := NewBroker(config, fixedSource{value: "secret"}, audit, nil)
	req := AccessRequest{AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/a/b/pulls"}
	if err := broker.WithCredential(context.Background(), req, func(Secret) error { return nil }); err == nil {
		t.Fatal("expected expired denial")
	}

	config = testConfig()
	audit = &memoryAudit{err: errors.New("ledger unavailable")}
	broker, _ = NewBroker(config, fixedSource{value: "secret"}, audit, nil)
	called := false
	if err := broker.WithCredential(context.Background(), req, func(Secret) error { called = true; return nil }); err == nil {
		t.Fatal("expected fail-closed audit error")
	}
	if called {
		t.Fatal("credential used without durable audit")
	}
}

func TestEnvironmentSourceDoesNotEchoSecretOrVariableNameInErrors(t *testing.T) {
	_ = os.Unsetenv("FD_TEST_MISSING_SECRET")
	_, err := (EnvironmentSource{}).Resolve(context.Background(), SecretSourceRef{Kind: "environment", Reference: "FD_TEST_MISSING_SECRET"})
	if err == nil {
		t.Fatal("expected missing secret")
	}
	if strings.Contains(err.Error(), "FD_TEST_MISSING_SECRET") {
		t.Fatal("error exposed source reference")
	}
}

func TestDestinationRuleNormalizationAndBoundary(t *testing.T) {
	rule := DestinationRule{Scheme: "HTTPS", Host: "API.GITHUB.COM.", PathPrefix: "/repos"}
	good := []string{"https://api.github.com/repos", "https://api.github.com/repos/acme/app"}
	bad := []string{"http://api.github.com/repos", "https://api.github.com/repos-evil", "https://api.github.com.evil/repos", "https://user@api.github.com/repos", "https://127.0.0.1/repos", "https://api.github.com:444/repos", "https://api.github.com/repos?token=x"}
	for _, u := range good {
		if !rule.Matches(u) {
			t.Errorf("expected match: %s", u)
		}
	}
	for _, u := range bad {
		if rule.Matches(u) {
			t.Errorf("unexpected match: %s", u)
		}
	}
}

func TestBrokerDeniesVerifiedAdapterUntilProcessIsolationExists(t *testing.T) {
	config := testConfig()
	config.Adapters = append(config.Adapters, AdapterIdentity{ID: "verified", Version: "0.1", TrustLevel: TrustVerified, ExecutableDigest: "sha256:abc", Enabled: true})
	config.Credentials[0].AllowedAdapters = append(config.Credentials[0].AllowedAdapters, "verified")
	audit := &memoryAudit{}
	broker, err := NewBroker(config, fixedSource{value: "secret"}, audit, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := AccessRequest{AdapterID: "verified", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/a/b/pulls"}
	if err := broker.WithCredential(context.Background(), req, func(Secret) error { return nil }); err == nil {
		t.Fatal("verified adapter should remain denied in v0.1")
	}
}

func TestBrokerRedactsSecretFromTrustedAdapterErrors(t *testing.T) {
	audit := &memoryAudit{}
	broker, err := NewBroker(testConfig(), fixedSource{value: "leaky-secret"}, audit, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := AccessRequest{AdapterID: "github", CredentialID: "github-main", Operation: "github.create_draft_pull_request", Destination: "https://api.github.com/repos/a/b/pulls"}
	err = broker.WithCredential(context.Background(), req, func(Secret) error { return errors.New("provider error includes leaky-secret") })
	if err == nil {
		t.Fatal("expected adapter error")
	}
	if strings.Contains(err.Error(), "leaky-secret") {
		t.Fatal("adapter error leaked secret")
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatal("expected redaction marker")
	}
}
