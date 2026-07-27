# ADR-024: The Go daemon is the authoritative orchestration implementation

**Status:** Accepted

The prior Node reference daemon validated the lifecycle but is no longer the primary code path. The Go daemon now owns transaction orchestration and must pass the same end-to-end invariants:

- private local transport;
- exact patch sealing;
- deterministic verification;
- digest-bound approval;
- integration-ref publication without changing the live checkout;
- abort and recovery;
- stale-source detection.
