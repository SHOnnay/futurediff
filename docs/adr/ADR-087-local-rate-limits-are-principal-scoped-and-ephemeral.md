# ADR-087 — Local rate limits are principal-scoped and ephemeral

Rate and concurrency controls are keyed by kernel-authenticated principal and maintained in daemon memory. Restart resets allowance state. Durable idempotency and transaction state—not rate state—remain the correctness boundary.
