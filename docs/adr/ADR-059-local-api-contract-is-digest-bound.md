# ADR-059: Local API compatibility is digest-bound

The daemon exposes `GET /v1/contract`. The versioned endpoint inventory marks which operations are safe for agent-facing integrations and produces a deterministic SHA-256 digest. `futurediff-api-contract` compares client and daemon digests before integration.
