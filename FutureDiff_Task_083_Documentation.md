# FutureDiff Task 083 — Storage-pressure mutation circuit breaker

## Objective

Keep recovery and diagnostics available while preventing new durable mutations when local storage is unsafe.

## Delivered

- `internal/storageguard` policy and cached evaluator.
- `futurediff-storage-check` command.
- Optional daemon `--storage-policy` configuration.
- Installer/service support for the policy.
- Signed-configuration compatibility (`storage_policy`).
- Health reporting without filesystem paths.
- Mutation middleware returning HTTP `507 Insufficient Storage`.

## Policy controls

- Minimum free bytes.
- Minimum free percentage.
- Maximum ledger bytes.
- Maximum managed-runtime bytes.

## Safety properties

The guard runs before idempotency reservation and mutation handlers. `GET`, `HEAD`, and `OPTIONS` remain available. Runtime-size traversal rejects symlinks.

## Validation

Unit tests used deterministic probes. Live validation started a daemon with an intentionally impossible free-space threshold: `POST /v1/transactions` returned `507`, while `GET /v1/openapi` returned `200`.
