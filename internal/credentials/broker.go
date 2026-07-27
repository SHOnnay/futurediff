package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type EnvironmentSource struct{}

func (EnvironmentSource) Resolve(_ context.Context, ref SecretSourceRef) (Secret, error) {
	if err := ref.Validate(); err != nil {
		return Secret{}, err
	}
	value, ok := os.LookupEnv(ref.Reference)
	if !ok || value == "" {
		return Secret{}, fmt.Errorf("configured environment secret is unavailable")
	}
	return newSecret(value), nil
}

type Broker struct {
	mu       sync.RWMutex
	adapters map[string]AdapterIdentity
	bindings map[string]Binding
	source   Source
	audit    AuditSink
	metadata MetadataSink
	now      func() time.Time
}

func NewBroker(config Config, source Source, audit AuditSink, metadata MetadataSink) (*Broker, error) {
	if source == nil {
		return nil, errors.New("credential source is required")
	}
	if audit == nil {
		return nil, errors.New("credential audit sink is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	b := &Broker{adapters: map[string]AdapterIdentity{}, bindings: map[string]Binding{}, source: source, audit: audit, metadata: metadata, now: func() time.Time { return time.Now().UTC() }}
	for _, adapter := range config.Adapters {
		b.adapters[adapter.ID] = adapter
		if metadata != nil {
			if err := metadata.RegisterAdapterIdentity(adapter); err != nil {
				return nil, err
			}
		}
	}
	for _, binding := range config.Credentials {
		normalized := binding
		normalized.AllowedAdapters = append([]string(nil), binding.AllowedAdapters...)
		normalized.AllowedOperations = append([]string(nil), binding.AllowedOperations...)
		normalized.AllowedDestinations = make([]DestinationRule, len(binding.AllowedDestinations))
		for i, d := range binding.AllowedDestinations {
			nd, _ := d.Normalize()
			normalized.AllowedDestinations[i] = nd
		}
		b.bindings[binding.ID] = normalized
		if metadata != nil {
			if err := metadata.RegisterCredentialBinding(normalized); err != nil {
				return nil, err
			}
		}
	}
	return b, nil
}

func (b *Broker) Status() map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()
	enabledAdapters, enabledCredentials := 0, 0
	for _, a := range b.adapters {
		if a.Enabled {
			enabledAdapters++
		}
	}
	for _, c := range b.bindings {
		if c.Enabled {
			enabledCredentials++
		}
	}
	return map[string]any{"configured": true, "adapter_count": len(b.adapters), "enabled_adapter_count": enabledAdapters, "credential_count": len(b.bindings), "enabled_credential_count": enabledCredentials, "secret_values_persisted": false}
}

func (b *Broker) WithCredential(ctx context.Context, req AccessRequest, use func(Secret) error) (err error) {
	if use == nil {
		return errors.New("credential consumer is required")
	}
	event := AccessEvent{EventID: newEventID(), TransactionID: req.TransactionID, EffectID: req.EffectID, AdapterID: req.AdapterID, CredentialID: req.CredentialID, Operation: req.Operation, Destination: req.Destination, CreatedAt: b.now()}
	deny := func(reason string, cause error) error {
		event.Decision = DecisionDenied
		event.Reason = reason
		if auditErr := b.audit.RecordCredentialAccess(event); auditErr != nil {
			return fmt.Errorf("credential access denied (%s); audit failed: %w", reason, auditErr)
		}
		if cause != nil {
			return fmt.Errorf("credential access denied: %s: %w", reason, cause)
		}
		return fmt.Errorf("credential access denied: %s", reason)
	}
	if strings.TrimSpace(req.AdapterID) == "" || strings.TrimSpace(req.CredentialID) == "" || strings.TrimSpace(req.Operation) == "" || strings.TrimSpace(req.Destination) == "" {
		return deny("incomplete access request", nil)
	}
	b.mu.RLock()
	adapter, adapterOK := b.adapters[req.AdapterID]
	binding, bindingOK := b.bindings[req.CredentialID]
	b.mu.RUnlock()
	if !adapterOK || !adapter.Enabled {
		return deny("adapter is unknown or disabled", nil)
	}
	if adapter.TrustLevel != TrustBuiltIn {
		return deny("only built-in adapters may access credentials in protocol 0.1", nil)
	}
	if !bindingOK || !binding.Enabled {
		return deny("credential is unknown or disabled", nil)
	}
	if binding.ExpiresAt != nil && !b.now().Before(binding.ExpiresAt.UTC()) {
		return deny("credential binding has expired", nil)
	}
	if !contains(binding.AllowedAdapters, req.AdapterID) {
		return deny("adapter is outside credential scope", nil)
	}
	if !contains(binding.AllowedOperations, req.Operation) {
		return deny("operation is outside credential scope", nil)
	}
	destinationAllowed := false
	for _, rule := range binding.AllowedDestinations {
		if rule.Matches(req.Destination) {
			destinationAllowed = true
			break
		}
	}
	if !destinationAllowed {
		return deny("destination is outside credential scope", nil)
	}

	secret, resolveErr := b.source.Resolve(ctx, binding.Source)
	if resolveErr != nil {
		event.Decision = DecisionError
		event.Reason = "secret source resolution failed"
		if auditErr := b.audit.RecordCredentialAccess(event); auditErr != nil {
			return fmt.Errorf("secret resolution and audit failed")
		}
		return errors.New("credential source resolution failed")
	}
	defer secret.Destroy()
	event.Decision = DecisionGranted
	event.Reason = "scope validated"
	if err := b.audit.RecordCredentialAccess(event); err != nil {
		return fmt.Errorf("credential access audit failed: %w", err)
	}
	if err := use(secret); err != nil {
		return fmt.Errorf("trusted adapter credential use failed: %s", redactSecret(err.Error(), secret.value))
	}
	return nil
}

func redactSecret(message string, secret []byte) string {
	if len(secret) == 0 {
		return message
	}
	return strings.ReplaceAll(message, string(secret), "[REDACTED]")
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func newEventID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "cred_evt_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("cred_evt_%d", time.Now().UnixNano())
}
