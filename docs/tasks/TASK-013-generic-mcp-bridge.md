# Task 013 — Generic MCP Stdio Bridge

**Status:** Completed  
**Primary language:** Go  
**Date:** 2026-07-27

## 1. Objective

Allow MCP-capable agent clients to use FutureDiff's staging and verification capabilities without embedding FutureDiff-specific logic into every agent and without granting the model approval or release authority.

## 2. Authority model

The MCP bridge is deliberately unprivileged.

Exposed:

- transaction creation and inspection;
- enforced workspace execution;
- sealing;
- deterministic verification;
- effect listing;
- GitHub branch preparation;
- GitHub PR preparation;
- Slack outbox preparation.

Not exposed:

- transaction approval;
- transaction commit;
- provider credential values;
- direct provider mutation APIs;
- verification-result overrides.

The trusted CLI/local API remains the release path.

## 3. Protocol implementation

Added:

```text
cmd/futurediff-mcp
internal/mcpbridge
```

The bridge uses:

- stdio transport;
- newline-delimited UTF-8 JSON-RPC messages;
- MCP protocol version `2025-11-25`;
- required `initialize` / `notifications/initialized` lifecycle;
- `ping`;
- `tools/list`;
- `tools/call`;
- 8 MiB inbound-message cap;
- protocol-only stdout;
- error results using `isError` for tool execution failures.

It translates tool calls to the private FutureDiff Unix-socket daemon API. The daemon remains authoritative.

## 4. Tools

```text
futurediff.transaction_create
futurediff.transaction_status
futurediff.transaction_execute
futurediff.transaction_seal
futurediff.transaction_verify
futurediff.effects_list
futurediff.github_branch_prepare
futurediff.github_pr_prepare
futurediff.slack_message_prepare
```

Tool annotations are descriptive hints only. FutureDiff's own daemon policy and adapter checks remain authoritative.

## 5. Safety controls

- Calls are rejected before successful initialization.
- Unknown or forbidden tool names return a tool error.
- Approval/commit names are absent from `tools/list`.
- A model cannot pass a transaction digest to a hidden commit path.
- The bridge does not load credential configuration.
- Secrets remain inside the daemon's brokered built-in adapter callbacks.
- Message size is bounded.
- Operational logs are written to stderr, not protocol stdout.

## 6. Validation

Passed:

```text
gofmt
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/futurediff-mcp
```

Process-level smoke validation:

```text
MCP protocol:             2025-11-25
Advertised tools:         9
Privileged tools exposed: no
Transaction created:      yes
Unix socket mode:         0600
```

Unit tests cover:

- initialization handshake;
- tools listing;
- tool-to-daemon routing;
- absence of approval and commit tools;
- forbidden tool behavior;
- calls before initialization.

## 7. Limitations

- This is a conservative zero-dependency bridge, not yet based on the official Go SDK.
- Streamable HTTP transport is not implemented.
- Sampling, prompts, resources, elicitation, and roots are not implemented.
- Client-specific configuration has not yet been certified across OpenCode, Hermes, Claude/Codex clients, and other hosts.
- Authentication is inherited from local OS access to the daemon Unix socket; multi-user server authentication is not implemented.
