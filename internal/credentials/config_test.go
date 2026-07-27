package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadConfigRequiresPrivatePermissionsAndRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.json")
	data := `{"version":"0.1","adapters":[{"adapter_id":"github","version":"0.1","trust_level":"built_in","executable_digest":"builtin:github@0.1","enabled":true}],"credentials":[{"credential_id":"github-main","provider":"github","source":{"kind":"environment","reference":"GITHUB_TOKEN"},"allowed_adapters":["github"],"allowed_operations":["github.create_draft_pull_request"],"allowed_destinations":[{"scheme":"https","host":"api.github.com","path_prefix":"/repos"}],"enabled":true}]}`
	if err := os.WriteFile(valid, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Adapters) != 1 || len(config.Credentials) != 1 {
		t.Fatalf("unexpected config: %#v", config)
	}

	if runtime.GOOS != "windows" {
		loose := filepath.Join(dir, "loose.json")
		if err := os.WriteFile(loose, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(loose); err == nil {
			t.Fatal("expected permission rejection")
		}
	}

	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"version":"0.1","adapters":[],"credentials":[],"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(unknown); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}
