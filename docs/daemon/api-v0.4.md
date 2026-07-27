# FutureDiff Local API v0.4

Transport: private HTTP over a Unix-domain socket. The daemon creates the socket with mode `0600`.

## Routes

```text
GET  /v1/health
POST /v1/transactions
GET  /v1/transactions/{id}
POST /v1/transactions/{id}/execute
POST /v1/transactions/{id}/effects/github/draft-pull-request
GET  /v1/transactions/{id}/effects
POST /v1/transactions/{id}/effects/{effect-id}/refresh
POST /v1/transactions/{id}/seal
POST /v1/transactions/{id}/verify
GET  /v1/transactions/{id}/approval-material
POST /v1/transactions/{id}/approve
POST /v1/transactions/{id}/commit
POST /v1/transactions/{id}/recover
POST /v1/transactions/{id}/abort
GET  /v1/transactions/{id}/events
```

## Prepare GitHub draft PR

`POST /v1/transactions/{id}/effects/github/draft-pull-request`

```json
{
  "credential_id": "github-main",
  "input": {
    "owner": "acme",
    "repo": "app",
    "title": "FutureDiff change",
    "body": "Prepared safely",
    "head": "feature/futurediff",
    "base": "main"
  }
}
```

The head and base branches must already exist remotely. Preparation reads and stores their exact SHAs.

## List effects

`GET /v1/transactions/{id}/effects`

Returns non-secret prepared-effect metadata and provider receipts.

## Refresh effect

`POST /v1/transactions/{id}/effects/{effect-id}/refresh`

Refreshes provider resource versions, increments material revision, clears approval, and returns the updated effect. Re-verification and re-approval are required.

## Commit behavior

A commit request requires the exact current transaction digest. Provider effects use write-ahead attempts, coordinator fencing, scoped broker grants, status-before-create, and reconciliation. A transaction with required external effects is not finalized until all have durable receipts.

## Credential policy

No API route returns a raw provider credential. Credential material is resolved only inside the built-in adapter callback after a durable access decision is recorded.
