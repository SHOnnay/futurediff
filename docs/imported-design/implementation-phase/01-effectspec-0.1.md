# EffectSpec 0.1 Draft

## Status

Draft for implementation-phase Step 01.

## Objective

EffectSpec defines the minimum contract a side-effecting adapter must expose so FutureDiff can stage, inspect, verify, commit, abort, recover, and audit effects across heterogeneous systems.

This spec is for adapter behavior and metadata. It is not a UI spec.

## Non-goals

EffectSpec 0.1 does not attempt to:
- create global ACID transactions across providers;
- guarantee rollback for irreversible systems;
- replace provider-native permission models;
- standardize every provider payload schema.

## Terms

- **transaction**: the parent unit coordinating one or more effects.
- **effect**: one side-effecting action under transaction control.
- **adapter**: the trusted implementation that bridges FutureDiff and a tool/provider.
- **prepared version**: the exact staged artifact or provider-side prepared state approved for commit.
- **resource URI**: canonical identifier for a resource touched by an effect.
- **receipt**: durable proof of prepare, commit, abort, or compensate outcome.
- **support level**: how strong the adapter’s guarantee really is.

## Required principles

Every conformant adapter MUST support these platform rules:

1. no hidden side effects outside the lifecycle contract;
2. durable effect identity;
3. idempotent commit when the provider supports it;
4. explicit declaration when idempotency is not available;
5. durable receipts for every irreversible lifecycle edge;
6. fail-closed behavior for unsupported operations.

## Effect classes

### `R` — reversible_local
Examples: file edits, worktree changes, generated artifacts, disposable DB migrations.

### `P` — provider_previewable
Examples: terraform plan, server-side dry run, payment authorization without capture.

### `O` — outboxable
Examples: Slack message, email, comment, webhook payload released from a durable outbox.

### `C` — compensatable
Examples: create issue, create calendar event, reversible cloud resource creation.

### `I` — irreversible
Examples: money transfer, uncontrolled external email, destructive external delete, physical action.

## Adapter support levels

Every adapter MUST declare one support level:

### `exact_prepare_commit`
Provider or adapter can commit the exact prepared version that was previewed and approved.

### `preview_with_freshness_check`
Preview is available, but commit cannot reference the exact prepared version. The adapter must perform version/freshness checks immediately before commit.

### `idempotent_best_effort`
No exact preview/prepare guarantee, but commit is idempotent and recoverable enough to participate in controlled flows.

### `unsupported`
The adapter is not safe enough for FutureDiff transactional guarantees.

Adapters at `unsupported` MUST NOT register as effectful commit paths.

## Lifecycle operations

Each adapter MUST expose these operations, though some MAY return `unsupported` with an explicit reason where allowed by policy.

- `describe`
- `prepare`
- `preview`
- `verify`
- `commit`
- `abort`
- `compensate`
- `status`

### `describe`
Returns static capability and risk metadata.

### `prepare`
Validates inputs, allocates effect identity, captures resource targets, and stages the effect without releasing the final external consequence.

### `preview`
Returns a structured description of the prepared effect. If an exact preview cannot be produced, the adapter must say so explicitly.

### `verify`
Runs adapter-specific checks required before approval or commit. This is not a replacement for transaction-level verification.

### `commit`
Releases the prepared effect or executes the approved effect path. Commit MUST emit a durable receipt or an explicit ambiguous outcome.

### `abort`
Discards any prepared but uncommitted state.

### `compensate`
Attempts an inverse action after commit when rollback is impossible. Compensation MUST be modeled separately from abort.

### `status`
Returns the best-known lifecycle state for recovery and reconciliation.

## Required `describe` metadata

An adapter description MUST include:

- `effectspec_version`
- `adapter_name`
- `adapter_version`
- `tool_name`
- `effect_class`
- `support_level`
- `mutates_state`
- `open_world`
- `destructive`
- `idempotent_commit`
- `resource_uri_patterns`
- `lifecycle_support`
- `freshness_check_support`
- `compensation_support`
- `redaction_ruleset`
- `timeout_policy`
- `retry_policy`
- `max_payload_size_bytes`
- `commit_priority`

## Required `prepare` output

`prepare` MUST return:

- `transaction_id`
- `effect_id`
- `adapter_name`
- `prepared_handle`
- `prepared_fingerprint`
- `resource_uris`
- `expires_at` when preparation has a TTL
- `idempotency_key` when supported
- `prepare_receipt`

## Required `preview` output

`preview` MUST return:

- `effect_id`
- `preview_format`
- `preview_summary`
- `preview_artifact_ref` when large
- `preview_fingerprint`
- `resource_versions` when available
- `limitations` when exact preview is not possible

## Required `commit` behavior

`commit` MUST:

- require `transaction_id` and `effect_id`;
- bind to the approved `prepared_fingerprint` or declared best-effort mode;
- reuse the stored idempotency key when applicable;
- record pre-commit freshness/version checks;
- emit a commit receipt or an `UNKNOWN` outcome;
- never silently downgrade from exact to weaker semantics.

## Required `abort` behavior

`abort` MUST:

- be safe to retry;
- record whether prepared state was fully removed;
- return a durable abort receipt or explicit reason why abort is not possible.

## Required `compensate` behavior

`compensate` MUST:

- declare whether a compensation path exists before commit;
- reference the original commit receipt;
- emit a separate compensation receipt;
- never claim rollback semantics.

## Required `status` behavior

`status` MUST return one of:

- `PREPARED`
- `COMMITTED`
- `ABORTED`
- `COMPENSATED`
- `UNKNOWN`
- `UNSUPPORTED`

Adapters MAY provide extra provider-native details, but the normalized state is mandatory.

## Approval binding requirements

The following values MUST be stable inputs to approval snapshots:

- `effect_id`
- `prepared_fingerprint`
- `preview_fingerprint`
- `resource_uris`
- `resource_versions` when available
- `support_level`
- adapter and policy versions

If any of these materially change after approval, the effect MUST be re-approved.

## Security requirements

A conformant adapter MUST NOT:

- expose raw effectful credentials to the agent;
- execute hidden side effects during `describe`, `preview`, or `verify`;
- mutate resources outside declared `resource_uris` unless the adapter explicitly expands them and records the expansion.

## Error model

Every lifecycle call MUST classify failure as one of:

- `VALIDATION_ERROR`
- `POLICY_BLOCKED`
- `PRECONDITION_FAILED`
- `TIMEOUT_AMBIGUOUS`
- `PROVIDER_ERROR`
- `UNSUPPORTED`
- `INTERNAL_ERROR`

`TIMEOUT_AMBIGUOUS` MUST map to recovery behavior, not blind retry.

## Minimal conformance expectations

An adapter is not implementation-ready until it passes tests for:

- stable `describe` metadata;
- prepare/preview determinism within one transaction;
- idempotent commit behavior where claimed;
- status recovery after simulated crash;
- abort safety before commit;
- compensation behavior where declared;
- fail-closed handling for unsupported paths.

## Draft example

```yaml
effectspec_version: "0.1"
adapter_name: github.merge_pull_request
adapter_version: "0.1.0"
tool_name: github.merge_pull_request

effect_class: C
support_level: preview_with_freshness_check

mutates_state: true
open_world: true
destructive: true
idempotent_commit: false

resource_uri_patterns:
  - "github://{owner}/{repo}/pull/{pull_number}"
  - "github://{owner}/{repo}/branch/{base_branch}"

lifecycle_support:
  describe: true
  prepare: true
  preview: true
  verify: true
  commit: true
  abort: true
  compensate: true
  status: true

freshness_check_support: true
compensation_support: create_revert_pull_request
redaction_ruleset: default-github

timeout_policy:
  prepare_seconds: 15
  commit_seconds: 30

retry_policy:
  commit_retry_mode: never_blind_retry
  status_poll_backoff: exponential

max_payload_size_bytes: 262144
commit_priority: 400
```

## Exit criteria for Step 01

Step 01 is complete when this draft is turned into:

- a final prose spec;
- a JSON Schema or equivalent typed contract;
- adapter conformance assertions tied to each required field and lifecycle rule.

## Immediate next step after this document

Freeze the transaction state machine so `prepare`, `commit`, `abort`, `status`, and `UNKNOWN` semantics have a single authoritative transition model.
