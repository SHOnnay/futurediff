package rootaudit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditRejectsSymlinkAndLooseRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	report := Audit(root, os.Geteuid(), time.Now())
	if report.Healthy {
		t.Fatal("expected loose root to fail")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "bad")); err != nil {
		t.Fatal(err)
	}
	report = Audit(root, os.Geteuid(), time.Now())
	if report.Healthy {
		t.Fatal("expected symlink to fail")
	}
}

func TestAuditHealthyRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ledger.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Audit(root, os.Geteuid(), time.Now())
	if !report.Healthy {
		t.Fatalf("%+v", report)
	}
}
