# ADR-080 — Unix peer identity is kernel authenticated

**Decision:** On Linux, FutureDiff authorizes local daemon clients using `SO_PEERCRED`, with socket permissions retained as defense in depth. HTTP-supplied identity headers are never trusted.
