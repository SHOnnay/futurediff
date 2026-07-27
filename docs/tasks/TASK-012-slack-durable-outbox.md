# Task 012 — Slack Durable Outbox

**Status:** Completed  
**Primary language:** Go  
**Date:** 2026-07-27

## 1. Objective

Add a message effect that is prepared and reviewed as part of the transaction but is not released to Slack until all dependencies and approval conditions are satisfied.

## 2. Transaction semantics

```text
prepare exact message
    → durable outbox record
    → include in verification and approval material
    → wait for effect dependencies
    → write provider attempt
    → status-before-post
    → post once
    → durable Slack receipt
```

Slack is intentionally late in the default commit order because a sent message has immediate social consequences.

## 3. Acceptance criteria

1. Store exact channel and message text during preparation.
2. Produce a stable idempotency/reconciliation identity.
3. Support effect dependencies.
4. Keep preparation free of provider mutation.
5. Use separate credential scopes for status and posting.
6. Query status before posting.
7. Write provider intent before posting.
8. Store a durable receipt on success.
9. Treat post transport errors as ambiguous.
10. Recover an accepted-but-unacknowledged message without a second post.
11. Never expose Slack credentials through API, events, logs, or SQLite.
12. Do not claim that message deletion reverses social consequences.

## 4. Implementation

### Built-in adapter

Added `internal/adapters/slackoutbox`.

Exact operations:

```text
slack.query_channel_history
slack.post_message
```

The adapter validates channel ID and message text, then prepares a request containing:

- exact channel;
- exact text;
- stable UUID-shaped `client_msg_id` derived from the FutureDiff effect ID;
- Slack metadata with event type `futurediff_effect` and the exact effect ID;
- request digest.

### Status reconciliation

Before posting and during recovery, FutureDiff reads bounded channel history and looks for:

- the exact `client_msg_id`; or
- the exact FutureDiff metadata effect marker.

If found, it creates a recovered receipt rather than posting again.

### HTTP security

The adapter:

- uses the credential broker callback;
- sends bearer authorization only to the approved Slack endpoint;
- disables redirects;
- avoids environment proxy inheritance;
- enforces bounded response bodies and timeouts;
- classifies transport/decode uncertainty as ambiguous.

### API and CLI

Added:

```text
POST /v1/transactions/{id}/effects/slack/message
```

CLI:

```text
prepare-slack-message
```

The command accepts zero or more dependency effect IDs.

## 5. Failure semantics

### Definite API rejection

The effect records a definite provider failure and enters reconciliation according to coordinator policy.

### Ambiguous accepted message

```text
POST accepted by Slack
    → connection resets
    → effect UNKNOWN
    → no blind second POST
    → recovery searches history
    → exact marker found
    → receipt stored
```

### Missing dependency

The message remains prepared and cannot release.

### Abort before release

Prepared outbox state is aborted without posting.

## 6. Validation

Passed:

```text
gofmt
go vet ./...
go test ./...
go test -race ./...
```

Key tests:

- prepare, post, and recover by metadata;
- stable client message identity;
- bearer credential required by fake provider;
- ambiguous transport classified correctly;
- ambiguous accepted message recovers with exactly one POST;
- transaction finalizes only after Slack receipt.

No real Slack workspace or message was used.

## 7. Limitations

- Status recovery currently uses bounded recent channel history; a production adapter should use the strongest provider-supported query/index strategy available.
- Message deletion/compensation is not implemented.
- Thread replies, blocks, attachments, uploads, reactions, and scheduled messages are outside v0.1.
- Real Slack test-workspace certification remains pending.
