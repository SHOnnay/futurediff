# ADR-089 — The data root is a local trust boundary

FutureDiff requires a private, correctly owned, non-symlink data root without writable or special top-level entries. This validation runs before daemon lock acquisition and ledger access so an unsafe filesystem boundary cannot be treated as a healthy deployment.
