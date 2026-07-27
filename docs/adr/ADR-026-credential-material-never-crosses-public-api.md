# ADR-026 — Credential material never crosses the public API

**Status:** Accepted

## Decision

FutureDiff stores only credential metadata and source-reference digests in SQLite. Secret values are resolved just in time and are passed only to a built-in adapter callback inside the trusted daemon process. The Unix-socket HTTP API exposes status counts only and has no endpoint that returns, previews, exports, or logs secret material.

## Consequences

- Transaction and audit records remain useful without containing provider tokens.
- A compromised UI or ordinary API client cannot request raw credentials.
- Provider adapters must execute through the broker rather than receive a token string from application code.
- Environment-backed sources are a bootstrap mechanism; OS keyring and secret-manager sources remain future work.
