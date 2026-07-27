package maintenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Version = "0.1"

type State struct {
	Version   string     `json:"version"`
	Enabled   bool       `json:"enabled"`
	Reason    string     `json:"reason,omitempty"`
	Actor     string     `json:"actor,omitempty"`
	EnabledAt *time.Time `json:"enabled_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Digest    string     `json:"digest"`
}

type Manager struct {
	Path string
	mu   sync.Mutex
}

func (m *Manager) Status(now time.Time) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(now)
}

func (m *Manager) Enable(reason, actor string, ttl time.Duration, now time.Time) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(reason) == "" {
		return State{}, errors.New("maintenance reason is required")
	}
	if strings.TrimSpace(actor) == "" {
		actor = "local-operator"
	}
	if ttl < 0 || ttl > 7*24*time.Hour {
		return State{}, errors.New("maintenance ttl must be between 0 and 168h")
	}
	n := now.UTC().Truncate(time.Second)
	st := State{Version: Version, Enabled: true, Reason: strings.TrimSpace(reason), Actor: strings.TrimSpace(actor), EnabledAt: &n}
	if ttl > 0 {
		expires := n.Add(ttl)
		st.ExpiresAt = &expires
	}
	st.Digest = digest(st)
	if err := m.writeLocked(st); err != nil {
		return State{}, err
	}
	return st, nil
}

func (m *Manager) Disable(actor string, now time.Time) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(actor) == "" {
		actor = "local-operator"
	}
	st := State{Version: Version, Enabled: false, Actor: strings.TrimSpace(actor)}
	st.Digest = digest(st)
	if err := m.writeLocked(st); err != nil {
		return State{}, err
	}
	return st, nil
}

func (m *Manager) MutationsAllowed(now time.Time) (bool, State, error) {
	st, err := m.Status(now)
	if err != nil {
		return false, State{}, err
	}
	return !st.Enabled, st, nil
}

func (m *Manager) statusLocked(now time.Time) (State, error) {
	if m == nil || strings.TrimSpace(m.Path) == "" {
		return State{Version: Version, Enabled: false, Digest: digest(State{Version: Version})}, nil
	}
	b, err := os.ReadFile(m.Path)
	if errors.Is(err, os.ErrNotExist) {
		st := State{Version: Version, Enabled: false}
		st.Digest = digest(st)
		return st, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := strictJSON(b, &st); err != nil {
		return State{}, fmt.Errorf("maintenance state: %w", err)
	}
	if st.Version != Version {
		return State{}, fmt.Errorf("unsupported maintenance state version %q", st.Version)
	}
	expected := digest(State{Version: st.Version, Enabled: st.Enabled, Reason: st.Reason, Actor: st.Actor, EnabledAt: st.EnabledAt, ExpiresAt: st.ExpiresAt})
	if st.Digest != expected {
		return State{}, errors.New("maintenance state digest mismatch")
	}
	if st.Enabled && st.ExpiresAt != nil && !now.UTC().Before(st.ExpiresAt.UTC()) {
		expired := State{Version: Version, Enabled: false, Actor: "automatic-expiry"}
		expired.Digest = digest(expired)
		if err := m.writeLocked(expired); err != nil {
			return State{}, err
		}
		return expired, nil
	}
	return st, nil
}

func (m *Manager) writeLocked(st State) error {
	if strings.TrimSpace(m.Path) == "" {
		return errors.New("maintenance state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, m.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func digest(st State) string {
	st.Digest = ""
	b, _ := json.Marshal(st)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err == nil {
		return errors.New("trailing JSON data")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
