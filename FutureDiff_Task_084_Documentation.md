# FutureDiff Task 084 — Deterministic OpenAPI 3.1 contract

## Objective

Provide a machine-readable API description that is cryptographically bound to FutureDiff's authoritative local API contract and authority classification.

## Delivered

- `internal/openapispec` deterministic generator and validator.
- `futurediff-openapi` command.
- `GET /v1/openapi` private daemon endpoint.
- OpenAPI 3.1 document digest.
- Binding to the canonical API-contract digest.
- Per-operation `x-futurediff-agent-safe` metadata.
- `/v1/openapi` added to API contract version 1.0 as an additive read operation.

## Validation rules

Validation rejects missing or unexpected routes, duplicate operations, operation-ID changes, agent-safety changes, contract-digest mismatch, and document-digest mismatch.

## Validation

Determinism and negative tests passed. The command fetched the document from a live Unix-socket daemon and verified it against the local contract.
