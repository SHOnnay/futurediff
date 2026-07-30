package guidedcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type unavailableEngine struct{}

func (unavailableEngine) Run(context.Context, ...string) ([]byte, error) {
	return nil, errors.New("not running")
}

func TestDaemonStartRejectsBroadCredentialConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics required")
	}
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := DaemonManager{Engine: unavailableEngine{}, Binary: "does-not-run", Root: t.TempDir(), CredentialConfig: path}
	err := manager.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("broad credential permissions were accepted: %v", err)
	}
}

func TestDaemonStartRejectsCredentialConfigSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "providers.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "providers-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	manager := DaemonManager{Engine: unavailableEngine{}, Binary: "does-not-run", Root: t.TempDir(), CredentialConfig: link}
	err := manager.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("credential symlink was accepted: %v", err)
	}
}
