# Task 010 — Durable External-Effect Coordinator and GitHub Draft-PR Adapter

**Status:** Completed  
**Primary language:** Go  
**Date:** 2026-07-27

## 1. Objective

Implement the first canonical external provider effect without exposing a provider credential to the agent or treating a network timeout as a normal retryable failure.

The effect selected for Task 010 is creation of a GitHub **draft pull request** for an already existing remote head branch.

## 2. Acceptance criteria

Task 010 is complete when:

1. a GitHub draft-PR effect can be prepared while the transaction is `ACTIVE`;
2. exact input, preview, request digest, destination, operation, and head/base SHAs are stored durably;
3. prepared effects are included in verification and approval material;
4. refreshing provider resource versions invalidates the old approval;
5. provider mutation requires a coordinator lease and fencing token;
6. commit intent is written before provider mutation;
7. ref reads, status queries, and create mutation receive separately scoped credential grants;
8. the adapter checks provider status before attempting creation;
9. a successful creation stores a durable receipt;
10. an ambiguous result enters `UNKNOWN` / reconciliation and is not blindly retried;
11. recovery can discover the created PR and finalize with one POST total;
12. a definite rejection can be re-armed after provider absence is proven;
13. a prepared effect can be aborted without provider mutation;
14. the transaction cannot become committed until both the repository and all required external effects are committed;
15. secret material does not appear in SQLite, events, previews, receipts, or API output.

All acceptance criteria were implemented and covered by automated tests available in this environment.

## 3. Implementation summary

### Domain

Added:

- `ExternalEffect`
- `EffectAttempt`
- `EffectReceipt`

These types contain only non-secret provider metadata and evidence.

### SQLite migration 0006

Added:

- `effect_documents`
- `effect_attempts`

Prepared documents store exact non-secret approval material. Attempts store write-ahead intent, fencing identity, and provider outcome classifications.

### External-effect coordinator

Implemented:

- create/list/get/refresh effect records;
- transaction-scoped leases;
- monotonic fencing tokens;
- write-ahead commit/status attempts;
- `UNKNOWN`, definite-failure, and committed outcomes;
- materialized-repository receipt tracking;
- final completion guard;
- abort behavior;
- reconciliation and re-arming.

### GitHub adapter

The built-in adapter:

- normalizes owner, repository, title, body, head, and base;
- rejects unsafe Git branch names;
- reads exact head/base SHAs;
- adds a unique effect marker to the draft body;
- produces an exact preview and request digest;
- rechecks provider freshness before mutation;
- queries open and closed PRs before creation;
- creates only draft PRs;
- limits response bodies;
- never follows redirects;
- ignores environment proxy variables by default;
- classifies transport/decode uncertainty as ambiguous;
- returns durable provider receipt material.

### Credential boundaries

Three exact operations are used:

```text
github.read_refs
github.query_pull_requests
github.create_draft_pull_request
```

A create credential grant cannot silently authorize read/status calls. Each access is independently audited.

### Approval binding

The verification and transaction digests now include prepared external-effect material. A provider-ref refresh changes the material revision and requires new verification and approval.

### API and CLI

Added API routes:

```text
POST /v1/transactions/{id}/effects/github/draft-pull-request
GET  /v1/transactions/{id}/effects
POST /v1/transactions/{id}/effects/{effect-id}/refresh
```

Added CLI commands:

```text
prepare-github-pr
effects
refresh-effect
```

## 4. Failure semantics

### Definite provider rejection

Example: GitHub returns HTTP 422 before creating a resource.

```text
attempt = definite_failure
transaction = needs_reconciliation
status query proves absence
transaction = ready
effect = verified
```

The user can correct the input or provider state and recommit after the appropriate approval lifecycle.

### Ambiguous transport result

Example: the connection resets after GitHub accepted the POST.

```text
attempt = unknown
transaction = needs_reconciliation
no second POST
status query finds effect marker
receipt persisted
transaction finalized
```

### Stale resource version

If head or base changes after approval:

```text
transaction = stale
approval cleared
provider POST count = 0
effect must be refreshed
verification and approval must be repeated
```

### Receipt persistence failure

If the provider committed but local receipt persistence fails, the transaction remains in reconciliation. It cannot be described as safely failed or blindly retried.

## 5. Validation performed

Executed successfully:

```text
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go test -cover ./...
go build ./cmd/futurediff
go build ./cmd/futurediffd
```

Key tests include:

- successful repository + draft-PR transaction;
- only one POST for normal commit;
- only one POST across ambiguous-outcome recovery;
- remote ref staleness blocks release and clears approval;
- definite rejection re-arms only after status proof;
- prepared-effect abort is durable and performs zero POSTs;
- effect refresh changes approval digest;
- full Unix-socket API lifecycle;
- provider credential absent from durable transaction events.

No real GitHub account or production token was used. Provider behavior was tested through a deterministic fake HTTPS transport.

## 6. Coverage snapshot

```text
effectspec                          55.6%
GitHub draft-PR adapter             68.9%
local API                           59.2%
application orchestration           63.6%
credential broker                   72.3%
domain                              47.4%
ledger                              27.0%
OCI runtime                         69.3%
Git staging                         63.2%
verification                        51.3%
```

Ledger coverage is the largest test-quality weakness and should be raised as external-effect functionality expands.

## 7. Honest limitations

1. The local FutureDiff integration commit is not pushed to the GitHub head branch.
2. The task assumes the head branch already exists remotely.
3. GitHub PR creation has no native idempotency key; reconciliation uses a unique effect marker plus exact resource matching.
4. The adapter does not merge, delete, force-push, publish, or mark ready for review.
5. Real rootless OCI execution remains uncertified in this environment.
6. No generic provider egress proxy exists yet.
7. Third-party adapters remain credential-ineligible.
8. No global ACID guarantee is claimed across Git and GitHub.

## 8. Next task

Task 011 should implement controlled GitHub remote branch publication and effect dependencies, ensuring the draft PR points to the exact approved FutureDiff result rather than an independently existing branch.
