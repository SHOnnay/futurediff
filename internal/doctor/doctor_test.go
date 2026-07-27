package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorCreatesAndAuditsPrivateDataRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fd")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), Options{DataRoot: root})
	if !report.Healthy {
		t.Fatalf("doctor unhealthy: %+v", report)
	}
	names := map[string]bool{}
	for _, c := range report.Checks {
		names[c.Name] = true
	}
	for _, name := range []string{"data_root", "git", "sqlite", "ledger"} {
		if !names[name] {
			t.Fatalf("missing %s check", name)
		}
	}
}
