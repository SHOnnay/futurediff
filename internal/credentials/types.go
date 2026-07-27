// Package credentials implements FutureDiff's credential-broker trust boundary.
// It deliberately separates secret material from durable metadata and audit data.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type TrustLevel string

const (
	TrustBuiltIn   TrustLevel = "built_in"
	TrustVerified  TrustLevel = "verified"
	TrustUntrusted TrustLevel = "untrusted"
)

type AdapterIdentity struct {
	ID               string     `json:"adapter_id"`
	Version          string     `json:"version"`
	TrustLevel       TrustLevel `json:"trust_level"`
	ExecutableDigest string     `json:"executable_digest"`
	Enabled          bool       `json:"enabled"`
}

func (a AdapterIdentity) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Version) == "" {
		return errors.New("adapter id and version are required")
	}
	switch a.TrustLevel {
	case TrustBuiltIn, TrustVerified, TrustUntrusted:
	default:
		return fmt.Errorf("unsupported adapter trust level %q", a.TrustLevel)
	}
	if a.TrustLevel != TrustUntrusted && strings.TrimSpace(a.ExecutableDigest) == "" {
		return errors.New("trusted adapters require an executable digest")
	}
	return nil
}

type SecretSourceRef struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}

func (s SecretSourceRef) Validate() error {
	if s.Kind != "environment" {
		return fmt.Errorf("unsupported secret source kind %q", s.Kind)
	}
	if strings.TrimSpace(s.Reference) == "" {
		return errors.New("secret source reference is required")
	}
	return nil
}

type DestinationRule struct {
	Scheme     string `json:"scheme"`
	Host       string `json:"host"`
	Port       string `json:"port,omitempty"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

func (d DestinationRule) Normalize() (DestinationRule, error) {
	d.Scheme = strings.ToLower(strings.TrimSpace(d.Scheme))
	d.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d.Host), "."))
	d.Port = strings.TrimSpace(d.Port)
	if d.Scheme != "https" {
		return DestinationRule{}, errors.New("credential destinations must use https")
	}
	if d.Host == "" {
		return DestinationRule{}, errors.New("destination host is required")
	}
	if strings.ContainsAny(d.Host, "/@?#") {
		return DestinationRule{}, errors.New("invalid destination host")
	}
	if ip := net.ParseIP(d.Host); ip != nil {
		return DestinationRule{}, errors.New("IP-literal credential destinations are rejected")
	}
	if d.Port != "" && d.Port != "443" {
		return DestinationRule{}, errors.New("only the default HTTPS port is allowed")
	}
	if d.PathPrefix == "" {
		d.PathPrefix = "/"
	}
	if !strings.HasPrefix(d.PathPrefix, "/") {
		return DestinationRule{}, errors.New("destination path prefix must be absolute")
	}
	cleaned := path.Clean(d.PathPrefix)
	if cleaned == "." {
		cleaned = "/"
	}
	d.PathPrefix = cleaned
	return d, nil
}

func (d DestinationRule) Matches(rawURL string) bool {
	normalized, err := d.Normalize()
	if err != nil {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return false
	}
	if strings.ToLower(u.Scheme) != normalized.Scheme {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host != normalized.Host {
		return false
	}
	port := u.Port()
	if port != "" && port != "443" {
		return false
	}
	p := path.Clean(u.EscapedPath())
	if p == "." || p == "" {
		p = "/"
	}
	prefix := normalized.PathPrefix
	if prefix == "/" {
		return true
	}
	return p == prefix || strings.HasPrefix(p, strings.TrimSuffix(prefix, "/")+"/")
}

type Binding struct {
	ID                  string            `json:"credential_id"`
	Provider            string            `json:"provider"`
	Account             string            `json:"account,omitempty"`
	Source              SecretSourceRef   `json:"source"`
	AllowedAdapters     []string          `json:"allowed_adapters"`
	AllowedOperations   []string          `json:"allowed_operations"`
	AllowedDestinations []DestinationRule `json:"allowed_destinations"`
	ExpiresAt           *time.Time        `json:"expires_at,omitempty"`
	Enabled             bool              `json:"enabled"`
}

func (b Binding) Validate() error {
	if strings.TrimSpace(b.ID) == "" || strings.TrimSpace(b.Provider) == "" {
		return errors.New("credential id and provider are required")
	}
	if err := b.Source.Validate(); err != nil {
		return err
	}
	if len(b.AllowedAdapters) == 0 || len(b.AllowedOperations) == 0 || len(b.AllowedDestinations) == 0 {
		return errors.New("credential binding requires adapters, operations, and destinations")
	}
	seen := map[string]bool{}
	for _, id := range b.AllowedAdapters {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return errors.New("allowed adapters must be unique and non-empty")
		}
		seen[id] = true
	}
	seen = map[string]bool{}
	for _, op := range b.AllowedOperations {
		op = strings.TrimSpace(op)
		if op == "" || seen[op] {
			return errors.New("allowed operations must be unique and non-empty")
		}
		seen[op] = true
	}
	for _, d := range b.AllowedDestinations {
		if _, err := d.Normalize(); err != nil {
			return err
		}
	}
	return nil
}

type Config struct {
	Version     string            `json:"version"`
	Adapters    []AdapterIdentity `json:"adapters"`
	Credentials []Binding         `json:"credentials"`
}

func (c Config) Validate() error {
	if c.Version != "0.1" {
		return fmt.Errorf("unsupported credential config version %q", c.Version)
	}
	adapterIDs := map[string]bool{}
	for _, a := range c.Adapters {
		if err := a.Validate(); err != nil {
			return fmt.Errorf("adapter %q: %w", a.ID, err)
		}
		if adapterIDs[a.ID] {
			return fmt.Errorf("duplicate adapter %q", a.ID)
		}
		adapterIDs[a.ID] = true
	}
	credentialIDs := map[string]bool{}
	for _, b := range c.Credentials {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("credential %q: %w", b.ID, err)
		}
		if credentialIDs[b.ID] {
			return fmt.Errorf("duplicate credential %q", b.ID)
		}
		credentialIDs[b.ID] = true
		for _, adapterID := range b.AllowedAdapters {
			if !adapterIDs[adapterID] {
				return fmt.Errorf("credential %q references unknown adapter %q", b.ID, adapterID)
			}
		}
	}
	return nil
}

func (c Config) Canonicalize() Config {
	out := c
	out.Adapters = append([]AdapterIdentity(nil), c.Adapters...)
	out.Credentials = append([]Binding(nil), c.Credentials...)
	sort.Slice(out.Adapters, func(i, j int) bool { return out.Adapters[i].ID < out.Adapters[j].ID })
	sort.Slice(out.Credentials, func(i, j int) bool { return out.Credentials[i].ID < out.Credentials[j].ID })
	return out
}

type AccessRequest struct {
	TransactionID string `json:"transaction_id,omitempty"`
	EffectID      string `json:"effect_id,omitempty"`
	AdapterID     string `json:"adapter_id"`
	CredentialID  string `json:"credential_id"`
	Operation     string `json:"operation"`
	Destination   string `json:"destination"`
}

type AccessDecision string

const (
	DecisionGranted AccessDecision = "granted"
	DecisionDenied  AccessDecision = "denied"
	DecisionError   AccessDecision = "error"
)

type AccessEvent struct {
	EventID       string         `json:"event_id"`
	TransactionID string         `json:"transaction_id,omitempty"`
	EffectID      string         `json:"effect_id,omitempty"`
	AdapterID     string         `json:"adapter_id"`
	CredentialID  string         `json:"credential_id"`
	Operation     string         `json:"operation"`
	Destination   string         `json:"destination"`
	Decision      AccessDecision `json:"decision"`
	Reason        string         `json:"reason"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Secret struct{ value []byte }

func newSecret(value string) Secret         { return Secret{value: []byte(value)} }
func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal("[REDACTED]") }
func (s Secret) CopyBytes() []byte          { return append([]byte(nil), s.value...) }
func (s *Secret) Destroy() {
	for i := range s.value {
		s.value[i] = 0
	}
	s.value = nil
}

type Source interface {
	Resolve(ctx context.Context, ref SecretSourceRef) (Secret, error)
}

type AuditSink interface {
	RecordCredentialAccess(event AccessEvent) error
}

type MetadataSink interface {
	RegisterAdapterIdentity(adapter AdapterIdentity) error
	RegisterCredentialBinding(binding Binding) error
}
