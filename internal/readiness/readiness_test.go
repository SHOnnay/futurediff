package readiness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.json")
	if err := os.WriteFile(p, []byte(`{"version":"0.1","require_audit_healthy":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil || !m.RequireAuditHealthy {
		t.Fatalf("%+v %v", m, err)
	}
}
