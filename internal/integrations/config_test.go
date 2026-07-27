package integrations

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeStrictConfig(t *testing.T) {
	raw, err := OpenCodeConfig(Options{MCPBinary: "/opt/futurediff/futurediff-mcp", Socket: "/home/u/.futurediff/futurediff.sock", Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	permission := v["permission"].(map[string]any)
	if permission["edit"] != "deny" || permission["bash"] != "deny" || permission["futurediff_*"] != "allow" {
		t.Fatalf("unsafe permissions: %#v", permission)
	}
	if strings.Contains(string(raw), "credential") {
		t.Fatal("config must not embed provider credentials")
	}
}

func TestHermesConfigFiltersTools(t *testing.T) {
	raw, err := HermesConfig(Options{MCPBinary: "/opt/futurediff/futurediff-mcp", Socket: "/home/u/.futurediff/futurediff.sock"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"mcp_servers:", "supports_parallel_tool_calls: false", "futurediff.transaction_create", "resources: false", "prompts: false"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %q", required)
		}
	}
	for _, forbidden := range []string{"approve", "commit", "token", "secret"} {
		if strings.Contains(text, "futurediff."+forbidden) {
			t.Fatalf("privileged tool leaked: %s", forbidden)
		}
	}
}

func TestOptionsRequireAbsolutePaths(t *testing.T) {
	if _, err := OpenCodeConfig(Options{MCPBinary: "futurediff-mcp", Socket: "/tmp/x"}); err == nil {
		t.Fatal("expected relative binary rejection")
	}
}
