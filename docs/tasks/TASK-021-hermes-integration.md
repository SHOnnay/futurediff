# Task 021 — Hermes Agent Integration

## Delivered

`futurediff-integrate --target hermes` generates a `mcp_servers.futurediff` YAML entry using stdio.

The generated entry:

- uses an explicit nine-tool include-list;
- disables MCP resources and prompts;
- disables parallel tool calls;
- includes no approval, commit, or credential tools;
- embeds no provider credential.

## Limitation

The generated configuration is structurally tested. A live Hermes session and reload cycle could not be certified because Hermes is not installed in this environment.

## Upstream references

- https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp
- https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference
