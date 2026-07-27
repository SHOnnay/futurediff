# ADR-022: Use Go as the primary implementation language

**Status:** Accepted

## Decision

The FutureDiff core, daemon, CLI, transaction coordinator, ledger integration, Git staging, verification engine, OCI runtime control, and EffectSpec SDK will be implemented in Go.

## Why

- fast implementation and review cycles;
- strong standard library for HTTP, Unix sockets, subprocesses, cryptography, and concurrency;
- straightforward single-command builds;
- low operational complexity;
- good fit for local daemons, gateways, and adapter processes;
- race detector available in normal CI.

This decision is about engineering velocity and maintainability. It is not a claim that Go is universally faster than Rust at runtime.

## Consequences

- Rust crates and the Node reference daemon are retired from the primary repository;
- the Go daemon becomes the conformance implementation;
- ecosystem-specific shims should be avoided when a generic MCP or process integration is possible;
- any unavoidable non-Go integration must remain outside the trusted core.
