# FutureDiff Corner Cases

These are the failure modes the architecture must handle before implementation starts.

## 1. Duplicate retry after timeout

**Case**: provider committed the effect, but the client lost the response and retries.

**Required design response**:
- durable idempotency keys per effect;
- `status` lookup before retrying `commit`;
- return prior receipt when the original commit already succeeded.

## 2. Preview/commit drift

**Case**: a PR target branch, database schema, ticket, or cloud resource changes after preview but before commit.

**Required design response**:
- pin resource versions during prepare/preview;
- re-check versions immediately before commit;
- force re-prepare and re-approval on drift.

## 3. Concurrent transactions touch same resource

**Case**: two agents modify the same repo branch, ticket, DB table, or Slack thread.

**Required design response**:
- canonical resource URIs;
- transaction-scoped locks with leases;
- policy for wait, fail, or manual arbitration.

## 4. Crash during partial commit

**Case**: GitHub effect committed, Slack effect not yet released.

**Required design response**:
- append-only ledger before and after each commit step;
- reconcile worker resumes from stored state;
- compensation only when policy says continuation is unsafe.

## 5. Ambiguous provider state

**Case**: timeout occurs and provider cannot immediately confirm whether the effect happened.

**Required design response**:
- explicit `UNKNOWN` effect state;
- delayed reconciliation polling;
- operator-visible manual intervention path.

## 6. Compensation fails

**Case**: revert PR creation fails, resource delete fails, or calendar cancellation is rejected.

**Required design response**:
- compensation outcomes logged separately from rollback;
- partial-compensation state in UI/export;
- reconciliation queue and operator escalation.

## 7. Irreversible effect mixed with reversible ones

**Case**: money transfer or external email is bundled with repo changes and DB migration.

**Required design response**:
- block by default;
- force a split transaction or separate approval boundary;
- require explicit policy override.

## 8. Agent bypasses the gateway

**Case**: the model receives raw credentials or can call the provider directly.

**Required design response**:
- gateway-exclusive credentials;
- network egress restrictions;
- audit alert when unproxied calls are detected.

## 9. Staged data leaks across transactions

**Case**: one transaction reads another transaction’s prepared Slack payload or DB migration.

**Required design response**:
- transaction-local staging namespaces;
- strict artifact isolation;
- no shared preview cache without transaction scoping.

## 10. Long-running database migration lock

**Case**: prepare passes in disposable Postgres, but real commit would lock production too long.

**Required design response**:
- migration risk classification;
- lock-time estimation and policy gates;
- optional online-migration adapter path.

## 11. Provider-generated payload mutation

**Case**: GitHub, Slack, or cloud provider adds defaults or transforms payloads between preview and commit.

**Required design response**:
- preserve provider preview output when available;
- compare final receipt against prepared payload hash;
- label non-exact providers as weaker guarantees.

## 12. Verification is nondeterministic

**Case**: tests rely on wall clock, network, race conditions, or flaky fixtures.

**Required design response**:
- capture verification environment metadata;
- separate deterministic failures from flaky failures;
- do not auto-approve nondeterministic evidence.

## 13. Policy changes after approval

**Case**: a transaction was approved under one policy bundle and commits under another.

**Required design response**:
- approval snapshot must include policy version hash;
- invalidate approval when policy changes materially affect the transaction.

## 14. Secret leakage in diff or evidence

**Case**: logs, env vars, or patches contain API keys or PII.

**Required design response**:
- mandatory redaction pass before display/export;
- encrypted raw artifact storage;
- role-based access to sensitive evidence.

## 15. Unsupported tool encountered mid-task

**Case**: the agent calls an unregistered webhook, browser extension, or custom shell script.

**Required design response**:
- fail closed;
- mark transaction unsupported;
- show the exact unsupported call and resource guess.

## 16. Nested agent or delegated sub-agent actions

**Case**: one agent calls another agent that performs effectful work.

**Required design response**:
- propagate parent transaction ID;
- forbid hidden child transactions by default;
- preserve causal chain across sub-agents.

## 17. Large artifact or diff volume

**Case**: massive logs, generated files, or DB diffs make UI/export unusable.

**Required design response**:
- store artifacts out-of-line in blob storage;
- stream previews with capped inline summaries;
- hash and chunk large evidence.

## 18. Idempotency key expires upstream

**Case**: provider only honors idempotency for a short time window.

**Required design response**:
- adapter-specific expiry metadata;
- commit within TTL or re-prepare;
- store provider receipt IDs for later status recovery.

## 19. Manual external changes during pending approval

**Case**: a human edits the repo branch, changes the issue, or posts the message separately.

**Required design response**:
- freshness re-check before commit;
- abort or re-stage on divergence;
- never silently overwrite manual changes.

## 20. Staged local code passes, live environment fails

**Case**: tests pass in container but production dependency versions or feature flags differ.

**Required design response**:
- environment fingerprint in evidence;
- mark guarantee scope honestly;
- optional deploy-target verification hooks.

## 21. Clock skew and lease expiry

**Case**: workers disagree on transaction lease validity.

**Required design response**:
- database-backed lease source of truth;
- monotonic renewal logic where possible;
- no commit on expired lease without reacquire.

## 22. Futurepack export becomes unsafe to share

**Case**: evidence package includes customer data, secrets, or internal URLs.

**Required design response**:
- redacted export default;
- sensitive export requires explicit privileged mode;
- per-artifact classification in manifest.

## 23. Outbox message semantically wrong but technically valid

**Case**: exact Slack preview is approved, but the wording is socially harmful or misleading.

**Required design response**:
- preserve this as a non-goal;
- require human review for external communications by default.

## 24. Provider preview exists but commit is not against exact prepared version

**Case**: a cloud plan is regenerated at commit rather than using the pinned plan file.

**Required design response**:
- reject the adapter as non-conformant for strong guarantees;
- downgrade the support level visibly if only best-effort preview exists.

## 25. Recovery loops forever

**Case**: status remains ambiguous and compensation keeps failing.

**Required design response**:
- bounded retry budgets;
- dead-letter reconciliation queue;
- explicit `FAILED_MANUAL_INTERVENTION` terminal state.

## Corner-case policy summary

Before implementation, treat these as mandatory product rules:

- unsupported effects fail closed;
- ambiguous effects become `UNKNOWN`, not assumed safe;
- approval is invalidated by material drift;
- irreversible effects are isolated;
- compensation is not rollback and must be shown separately;
- recovery is part of the normal lifecycle, not an exception path.
