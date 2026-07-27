# FutureDiff local API v0.2

Transport: HTTP/1.1 over a private Unix-domain socket. The daemon creates the socket with mode `0600` and does not open a TCP listener.

## Endpoints

```text
GET  /v1/health
POST /v1/transactions
GET  /v1/transactions/{id}
POST /v1/transactions/{id}/execute
POST /v1/transactions/{id}/seal
POST /v1/transactions/{id}/verify
GET  /v1/transactions/{id}/approval-material
POST /v1/transactions/{id}/approve
POST /v1/transactions/{id}/commit
POST /v1/transactions/{id}/recover
POST /v1/transactions/{id}/abort
GET  /v1/transactions/{id}/events
```

`execute` is available only for enforced transactions and requires a configured rootless-ready OCI runtime. It executes an agent-authored command in a sanitized copied workspace, records evidence, and synchronizes changes into the transaction worktree only after a successful mutation result.

Approval and commit requests must provide the exact transaction digest returned by `approval-material`. A change in patch, verification evidence, policy version, material revision, or pinned source version invalidates the old digest.
