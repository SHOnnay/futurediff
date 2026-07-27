# Task 035 — Local API contract and compatibility check

## Implemented

- `GET /v1/contract` daemon endpoint
- versioned endpoint and authority inventory
- deterministic API-contract digest
- JSON Schema for the contract
- `futurediff-api-contract` client/daemon compatibility command
- tests ensuring approval and commit endpoints are never marked agent-safe

Example:

```bash
futurediff-api-contract --socket ~/.futurediff/futurediff.sock
```

A digest mismatch exits nonzero and prevents integrations from assuming incompatible semantics.
