# ADR-038 — MCP Bridge Excludes Approval and Commit Authority

**Status:** Accepted

## Decision

The generic MCP tool surface allows staging, inspection, verification, and effect preparation, but does not expose transaction approval or commit.

## Rationale

The model or agent client is part of the proposal path, not the trusted release path. Exposing commit as an MCP tool would collapse the primary trust boundary.

## Consequences

- a trusted local user/policy path must complete release;
- agents can autonomously build a reviewable future but cannot make it real;
- future delegated approval requires a separate identity, policy, and authentication design.
