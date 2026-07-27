# ADR-036 — Create-Only GitHub Branch Publication

**Status:** Accepted

## Decision

The built-in branch adapter may only create absent `futurediff/*` branches pointing to the exact approved commit. Existing refs are never updated.

## Rationale

Create-only publication prevents an autonomous workflow from overwriting collaborator history and gives each transaction a stable remote identity.

## Consequences

- retries require status reconciliation;
- conflicts require a new branch/effect rather than force-push;
- branch cleanup is a separate future compensating operation.
