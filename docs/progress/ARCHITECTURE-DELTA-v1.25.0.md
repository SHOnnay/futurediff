# FutureDiff Architecture v12.5 — Production Assurance Overlay

## Assurance chain

```text
source tree
  -> strict regular-file walk
  -> canonical SHA-256 manifest
  -> secret and license policy
  -> CycloneDX SBOM
  -> SLSA-compatible provenance
  -> unit, recovery, chaos, and SLO checks
  -> production-readiness decision
  -> deterministic release archive
  -> archive verification
  -> optional detached signature
  -> hosted artifact attestation
```

## Fail-closed properties

- symbolic links and special files are rejected;
- archive traversal and link entries are rejected;
- missing required files, commands, or external evidence fail readiness;
- release creation stops when readiness, recovery, or chaos checks fail;
- signatures and attestations are additional evidence, not replacements for source verification;
- simulated checks never certify unavailable providers or hosts.

## Integration boundary

This is a source overlay for the canonical FutureDiff repository. It adds production-assurance tooling without replacing the transaction kernel, ledger, adapter state machines, credential broker, OCI runtime, or external-effect coordinator.
