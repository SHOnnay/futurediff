# ADR-065: Signed operator approvals are expiring and keyring-bound

Status: accepted

Unsigned local approval remains available for development unless the daemon is started with `--require-signed-approvals`. In strict mode, approval must be an Ed25519 envelope whose transaction ID and approval-material digest match the current ledger state. The signer must exist in a trusted keyring, the approver identity must match the key record, and the envelope must not be expired.

Only a signature reference, approver identity, and expiry are stored in the ledger. Private keys never enter the daemon or ledger. Approval envelopes are invalidated naturally when transaction material changes because the signed digest no longer matches.
