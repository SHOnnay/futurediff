package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnableDisableExpiryAndTamper(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	m := &Manager{Path: filepath.Join(t.TempDir(), "maintenance.json")}
	st, err := m.Enable("backup", "alice", time.Hour, now)
	if err != nil || !st.Enabled {
		t.Fatalf("enable: %v %+v", err, st)
	}
	allowed, _, err := m.MutationsAllowed(now.Add(time.Minute))
	if err != nil || allowed {
		t.Fatalf("expected blocked: %v", err)
	}
	allowed, expired, err := m.MutationsAllowed(now.Add(2 * time.Hour))
	if err != nil || !allowed || expired.Enabled {
		t.Fatalf("expiry: %v %+v", err, expired)
	}
	if _, err := m.Enable("upgrade", "bob", 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Disable("bob", now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path, []byte(`{"version":"0.1","enabled":true,"digest":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Status(now); err == nil {
		t.Fatal("tamper accepted")
	}
}
