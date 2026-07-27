# ADR-084 — API audit stores digests, not payloads

**Decision:** Mutation audit records contain authenticated principal, route, result and cryptographic request/key identities; bodies, keys and response content are excluded.
