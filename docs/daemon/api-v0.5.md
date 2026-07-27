# FutureDiff Local API v0.5

Transport: private HTTP over a Unix-domain socket with mode `0600`.

## Core routes

```text
GET    /v1/health
POST   /v1/transactions
GET    /v1/transactions/{id}
POST   /v1/transactions/{id}/execute
POST   /v1/transactions/{id}/seal
POST   /v1/transactions/{id}/verify
GET    /v1/transactions/{id}/approval-material
POST   /v1/transactions/{id}/approve
POST   /v1/transactions/{id}/commit
POST   /v1/transactions/{id}/recover
POST   /v1/transactions/{id}/abort
GET    /v1/transactions/{id}/events
```

## External-effect routes

```text
POST   /v1/transactions/{id}/effects/github/branch
POST   /v1/transactions/{id}/effects/github/draft-pull-request
POST   /v1/transactions/{id}/effects/slack/message
GET    /v1/transactions/{id}/effects
POST   /v1/transactions/{id}/effects/{effectID}/refresh
```

## Authority

The local API contains approval and commit routes. The generic MCP bridge intentionally exposes only a restricted subset and does not route approval or commit.

## Limits

- JSON request bodies are limited to 1 MiB by the HTTP decoder.
- Unknown JSON fields are rejected.
- path traversal is rejected.
- local multi-user authentication is not yet implemented; access relies on Unix socket ownership and mode.
