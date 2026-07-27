package configlint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerificationLint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "verify.json")
	valid := `{"format_version":"0.1","contract_id":"c","policy_version":"p","checks":[{"check_id":"x","required":true,"executor":"workspace_assertion","type":"file_exists","path":"README.md"}]}`
	if err := os.WriteFile(p, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if r := Lint(p, "verification"); !r.Valid {
		t.Fatalf("%+v", r)
	}
	if err := os.WriteFile(p, []byte(`{"format_version":"0.1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if r := Lint(p, "verification"); r.Valid {
		t.Fatal("expected invalid")
	}
}
func TestOpenCodeRejectsAuthority(t *testing.T) {
	p := filepath.Join(t.TempDir(), "open.json")
	data := `{"mcp":{"futurediff":{"type":"local","command":["/bin/futurediff-mcp","--socket","/tmp/fd.sock"]}},"permission":{"transaction_commit":"allow"}}`
	os.WriteFile(p, []byte(data), 0600)
	if r := Lint(p, "opencode"); r.Valid {
		t.Fatal("expected forbidden authority rejection")
	}
}
