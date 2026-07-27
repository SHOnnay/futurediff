# ADR-075: Operator actions are signed and hash-chained

Administrative actions may be recorded as immutable append-only JSON receipts. Each receipt binds the previous digest, operator identity, action, subject, reason, timestamp, and Ed25519 signature. The receipt store is an audit aid, not an external transparency log.
