package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildAndApplyPlan(t *testing.T) {
	src := t.TempDir()
	prefix := filepath.Join(t.TempDir(), "prefix")
	data := filepath.Join(t.TempDir(), "data")
	for _, n := range []string{"futurediff", "futurediffd", "futurediff-mcp"} {
		if err := os.WriteFile(filepath.Join(src, n), []byte(n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	service := ServiceNone
	plan, err := BuildPlan(Options{SourceDir: src, Prefix: prefix, DataRoot: data, Socket: filepath.Join(data, "fd.sock"), Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "futurediffd")); err != nil {
		t.Fatal(err)
	}
}
func TestServiceRenderDoesNotEmbedSecrets(t *testing.T) {
	o := Options{Prefix: "/tmp/prefix", DataRoot: "/tmp/data", Socket: "/tmp/data/fd.sock", CredentialConfig: "/tmp/credentials.json"}
	s := renderSystemd(o)
	if strings.Contains(s, "TOKEN") {
		t.Fatal("service contains secret")
	}
	if !strings.Contains(s, "--credential-config") {
		t.Fatal("credential metadata path missing")
	}
}
func TestWrongServiceOSRejected(t *testing.T) {
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "futurediffd"), []byte("x"), 0o755)
	service := ServiceSystemd
	if runtime.GOOS == "linux" {
		service = ServiceLaunchd
	}
	_, err := BuildPlan(Options{SourceDir: src, Prefix: filepath.Join(t.TempDir(), "p"), DataRoot: filepath.Join(t.TempDir(), "d"), Socket: filepath.Join(t.TempDir(), "s"), Service: service})
	if err == nil {
		t.Fatal("expected OS-specific service rejection")
	}
}
