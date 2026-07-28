# FutureDiff Architecture v14.0 — Operational Assurance Overlay

## Assurance layers

```text
transaction platform
  -> source and release assurance
  -> deployment contract
  -> environment parity
  -> compatibility evidence
  -> upgrade and rollback safety
  -> capacity and soak thresholds
  -> observability and alert routing
  -> data governance
  -> incident readiness
  -> release approval quorum
  -> evidence catalog
  -> deterministic operational bundle
  -> local production gate
  -> external certification boundary
```

## New components

- `tools/futurediff_operations.py`: standard-library operational assurance CLI;
- deployment, parity, compatibility, capacity, soak, observability, alerting, data-governance, incident, and approval policies;
- deterministic operational evidence packaging;
- `scripts/operations-assurance.sh` and PowerShell core wrapper;
- unified local production gate;
- cross-platform operational-assurance workflow.

## Fail-closed properties

- production topology must declare durable storage, queue semantics, observability, backups, and replica floors;
- passed compatibility rows require SHA-256 evidence identifiers;
- migration and deployment steps require explicit rollback references;
- unknown outcomes must remain within policy, normally zero;
- credential-bearing log fields are forbidden;
- credential data may be stored only through the credential broker;
- release approval requires distinct actors, quorum, role coverage, and digest binding;
- cataloged evidence must be regular files and is rehashed immediately before bundling;
- bundle traversal, links, missing files, and content mutation are rejected;
- the unified result is labeled local-only and cannot suppress the external-certification requirement.

## Integration boundary

This overlay does not replace the canonical transaction kernel or provide real provider and production infrastructure evidence. It supplies deterministic local controls that must be applied to, and executed against, the canonical repository and production-like systems.
