# Task 039 — Configuration Linting

## Goal

Reject malformed or authority-expanding configuration before it reaches the daemon or an agent.

## Implemented

`futurediff-config-lint` supports:

- `credentials`
- `verification`
- `agent-run`
- `installer-plan`
- `opencode`
- generic strict `json`
- `auto` detection

Checks include strict JSON decoding, unknown fields where typed schemas exist, credential-file permission validation, verification DAG validation, measured-run constraints, installer target uniqueness and absolute paths, and OpenCode MCP command/authority restrictions.

## Security behavior

The OpenCode linter rejects profiles containing approval, commit, or credential authority. It verifies that the local MCP command launches `futurediff-mcp` with a daemon socket.

## Validation

Valid verification and generated strict OpenCode profiles passed. Incomplete verification contracts and an authority-expanding OpenCode profile failed.
