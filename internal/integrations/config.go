package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	MCPBinary string
	Socket    string
	Strict    bool
}

func (o Options) Validate() error {
	if strings.TrimSpace(o.MCPBinary) == "" || strings.TrimSpace(o.Socket) == "" {
		return errors.New("MCP binary and daemon socket are required")
	}
	if !filepath.IsAbs(o.MCPBinary) || !filepath.IsAbs(o.Socket) {
		return errors.New("MCP binary and daemon socket must be absolute paths")
	}
	return nil
}

func OpenCodeConfig(o Options) ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	permission := map[string]any{"futurediff_*": "allow"}
	if o.Strict {
		permission["*"] = "ask"
		permission["edit"] = "deny"
		permission["bash"] = "deny"
	}
	config := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{
			"futurediff": map[string]any{
				"type":    "local",
				"command": []string{o.MCPBinary, "--socket", o.Socket},
				"enabled": true,
				"timeout": 10000,
			},
		},
		"permission": permission,
	}
	return json.MarshalIndent(config, "", "  ")
}

func HermesConfig(o Options) ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	tools := []string{
		"futurediff.transaction_create",
		"futurediff.transaction_status",
		"futurediff.transaction_execute",
		"futurediff.transaction_seal",
		"futurediff.transaction_verify",
		"futurediff.effects_list",
		"futurediff.github_branch_prepare",
		"futurediff.github_pr_prepare",
		"futurediff.slack_message_prepare",
	}
	var b strings.Builder
	fmt.Fprintln(&b, "mcp_servers:")
	fmt.Fprintln(&b, "  futurediff:")
	fmt.Fprintf(&b, "    command: %q\n", o.MCPBinary)
	fmt.Fprintln(&b, "    args:")
	fmt.Fprintln(&b, "      - --socket")
	fmt.Fprintf(&b, "      - %q\n", o.Socket)
	fmt.Fprintln(&b, "    enabled: true")
	fmt.Fprintln(&b, "    timeout: 120")
	fmt.Fprintln(&b, "    connect_timeout: 20")
	fmt.Fprintln(&b, "    supports_parallel_tool_calls: false")
	fmt.Fprintln(&b, "    tools:")
	fmt.Fprintln(&b, "      include:")
	for _, tool := range tools {
		fmt.Fprintf(&b, "        - %s\n", tool)
	}
	fmt.Fprintln(&b, "      resources: false")
	fmt.Fprintln(&b, "      prompts: false")
	return []byte(b.String()), nil
}

func WriteAtomic(path string, data []byte) error {
	if path == "" {
		return errors.New("output path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}
