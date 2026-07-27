# Approval Snapshot Contract 0.1 Draft

## Status

Draft for implementation-phase Step 03.

## Objective

This document freezes the approval artifact that binds FutureDiff approval to one exact prepared future. Approval in FutureDiff must never mean “approve whatever the system computes later.” It must mean “approve this exact staged transaction snapshot under these exact rules.”

## Design principles

1. Approval MUST bind to a specific prepared snapshot.
2. Approval MUST be invalidated by material drift.
3. Approval MUST capture both human-readable intent and machine-verifiable hashes.
4. Approval MUST be portable enough for audit/export.
5. Approval MUST not be reusable across transactions.

## Scope

This spec defines:
- approval snapshot contents;
- required hashes and references;
- approver record fields;
- invalidation triggers;
- commit preconditions tied to approval.

It does not yet define:
- the human approval UI;
- signature transport details;
- cross-tenant identity federation.

## Core rule

A transaction may move from `AWAITING_APPROVAL` to `READY_TO_COMMIT` only when an approval snapshot exists for the current prepared transaction state and every approval-binding field still matches at commit time.

## What an approval snapshot represents

An approval snapshot is a sealed record of:
- the transaction identity;
- the exact effect set under approval;
- the prepared and preview fingerprints for each effect;
- the resource versions and commit order;
- the verification evidence set;
- the applicable policy bundle;
- the approval decision and approver identity.

## Required top-level fields

Every approval snapshot MUST contain:

- `snapshot_version`
- `snapshot_id`
- `transaction_id`
- `transaction_fingerprint`
- `created_at`
- `expires_at` when approval TTL exists
- `approval_mode`
- `approval_decision`
- `policy_bundle_version`
- `policy_bundle_hash`
- `verification_bundle_hash`
- `effect_set_hash`
- `commit_plan_hash`
- `resource_set_hash`
- `approver_records`
- `effect_bindings`
- `transaction_summary`

## Required transaction-level bindings

### `transaction_fingerprint`
Hash over the canonical transaction approval payload. It MUST change when any material approval input changes.

### `effect_set_hash`
Hash over the ordered set of included effect IDs plus their binding fields.

### `commit_plan_hash`
Hash over the exact ordered commit plan. If commit order changes, approval is invalid.

### `resource_set_hash`
Hash over the canonical ordered list of resource URIs and pinned versions where available.

### `verification_bundle_hash`
Hash over the verification evidence references and outcome summary included in the approval decision.

## Required effect binding fields

Each `effect_bindings[]` entry MUST include:

- `effect_id`
- `adapter_name`
- `adapter_version`
- `effect_class`
- `support_level`
- `prepared_fingerprint`
- `preview_fingerprint`
- `preview_summary_hash`
- `resource_uris`
- `resource_versions` when available
- `idempotency_key_ref` when commit uses one
- `verification_refs`
- `commit_priority`
- `limitations` when support level is weaker than exact prepare/commit

These fields are the minimum per-effect approval boundary.

## Required transaction summary fields

`transaction_summary` MUST contain a compact human-readable summary of:

- transaction purpose;
- effect counts by class;
- external systems touched;
- destructive or irreversible flags;
- verification outcome summary;
- commit sequence summary;
- compensation availability summary;
- known limitations.

This summary is for review ergonomics, but it is not the source of truth. The hashes and binding fields are.

## Required approver record fields

Each `approver_records[]` entry MUST contain:

- `approver_type` (`human`, `policy`, or `mixed`)
- `approver_id`
- `approver_display`
- `decision` (`approved` or `rejected`)
- `decision_reason`
- `approved_at`
- `auth_context_ref`
- `signature_or_attestation_ref`

If approval is policy-only, the system MUST still create an approver record with policy identity and evaluation reference.

## Approval modes

### `human_required`
At least one human approver record is mandatory.

### `policy_only`
No human is required, but the approval snapshot still binds to the exact prepared future.

### `human_plus_policy`
Both policy evaluation and human approval are required.

`approval_mode` MUST be recorded in the snapshot and enforced at transition time.

## Canonical hash inputs

All approval hashes MUST be derived from canonical serialized data.

Minimum canonicalization rules:

- UTF-8 encoding;
- stable key ordering for maps/objects;
- stable array ordering where order is semantically meaningful;
- explicit omission or null rules defined by schema;
- no hash over presentation-only formatting.

FutureDiff MUST publish one canonical serializer for approval hashing. Adapters and services must not improvise this.

## Transaction fingerprint contents

`transaction_fingerprint` MUST include at least:

- `transaction_id`
- `approval_mode`
- `policy_bundle_hash`
- `verification_bundle_hash`
- `effect_set_hash`
- `commit_plan_hash`
- `resource_set_hash`
- any declared approval TTL

This is the top-level machine check that the approved future still matches the pending commit.

## Commit preconditions tied to approval

Before entering `READY_TO_COMMIT` and again before `COMMITTING`, the coordinator MUST verify that:

- snapshot exists and is not revoked;
- approval mode requirements are satisfied;
- approval decision is `approved`;
- snapshot has not expired when TTL applies;
- policy bundle hash still matches;
- transaction fingerprint still matches current prepared state;
- no effect binding field changed materially;
- no required verification evidence was superseded by a failed rerun.

## Invalidation rules

Approval MUST be invalidated if any of the following change materially after snapshot creation:

- effect added or removed;
- effect order in the commit plan changes;
- `prepared_fingerprint` changes for any effect;
- `preview_fingerprint` changes for any effect;
- `resource_uris` change;
- pinned `resource_versions` change;
- `support_level` changes;
- `adapter_version` changes when semantics may differ;
- policy bundle hash changes;
- verification bundle hash changes;
- approval TTL expires.

On invalidation, the transaction MUST move back to `ACTIVE` and the ledger MUST record the invalidation reason.

## Re-approval rules

Re-approval is required when approval is invalidated. The system MUST create a new snapshot ID. It MUST NOT mutate the prior snapshot in place.

Previous snapshots remain audit artifacts and MUST be retained.

## Drift checks during `AWAITING_APPROVAL`

While a transaction waits for approval, the coordinator SHOULD perform cheap drift checks for:

- resource version changes;
- policy bundle version changes;
- effect set changes caused by agent repair;
- expiration of prepared handles or idempotency windows.

Detected drift MUST invalidate the pending snapshot or block approval creation.

## Security requirements

An approval snapshot MUST NOT include raw secrets by default.

It MAY include:
- redacted summaries;
- secret references;
- encrypted artifact references;
- capability-safe evidence references.

Approval artifacts intended for export MUST support redacted and privileged views.

## Portability requirements

Approval snapshots MUST be portable into:
- local audit logs;
- `.futurepack` exports;
- benchmark evidence packages;
- recovery workflows after restart.

Portability does not mean every artifact is plaintext. Sensitive content may remain encrypted behind references.

## Minimal machine-readable shape

A typed contract for this spec SHOULD include at least:

```yaml
snapshot_version: "0.1"
snapshot_id: as_01...
transaction_id: tx_01...
transaction_fingerprint: sha256:...
created_at: "2026-07-26T12:00:00Z"
expires_at: "2026-07-26T12:30:00Z"
approval_mode: human_plus_policy
approval_decision: approved
policy_bundle_version: "2026.07.26"
policy_bundle_hash: sha256:...
verification_bundle_hash: sha256:...
effect_set_hash: sha256:...
commit_plan_hash: sha256:...
resource_set_hash: sha256:...
approver_records:
  - approver_type: human
    approver_id: user_123
    approver_display: Sakib
    decision: approved
    decision_reason: Approves verified staged transaction.
    approved_at: "2026-07-26T12:05:00Z"
    auth_context_ref: authctx_01...
    signature_or_attestation_ref: sig_01...
effect_bindings:
  - effect_id: eff_01...
    adapter_name: github.create_pull_request
    adapter_version: "0.1.0"
    effect_class: C
    support_level: preview_with_freshness_check
    prepared_fingerprint: sha256:...
    preview_fingerprint: sha256:...
    preview_summary_hash: sha256:...
    resource_uris:
      - github://owner/repo/pull/new
      - github://owner/repo/branch/main
    resource_versions:
      - resource_uri: github://owner/repo/branch/main
        version: sha:abc123
    idempotency_key_ref: idem_01...
    verification_refs:
      - vr_01...
    commit_priority: 400
    limitations:
      - Must re-check base branch freshness before commit.
transaction_summary:
  purpose: Add required customer_status field, prepare PR, prepare Slack notification.
  effect_counts:
    R: 2
    C: 1
    O: 1
  external_systems:
    - postgres
    - github
    - slack
  destructive: false
  irreversible: false
  verification_outcome: passed
  commit_sequence:
    - repository
    - database
    - github
    - slack
  compensation_available: true
  known_limitations:
    - GitHub branch freshness must be re-checked before commit.
```

## Approval decision semantics

### Approved
Means the exact bound snapshot may proceed if freshness and lease checks still pass.

### Rejected
Means the current prepared snapshot must not commit. The transaction may return to `ACTIVE` for repair or `ABORTING` by policy.

A rejection MUST preserve the snapshot as an audit artifact.

## Relationship to Step 02 state machine

This contract is what makes these transitions meaningful:

- `AWAITING_APPROVAL -> READY_TO_COMMIT`
- `READY_TO_COMMIT -> ACTIVE` on drift/invalidation

Without this contract, those transitions are too vague for implementation.

## Exit criteria for Step 03

Step 03 is complete when this draft is turned into:
- a final prose spec;
- a machine-readable schema;
- canonical hash rules shared by all approval producers/consumers;
- tests proving approval invalidates on every material drift case.

## Immediate next step after this document

Freeze the canonical resource URI contract so locking, drift detection, and audit references all point at the same resource identity model.
