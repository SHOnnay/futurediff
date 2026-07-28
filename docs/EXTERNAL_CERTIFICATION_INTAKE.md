# External Certification Intake

FutureDiff treats external certification evidence as untrusted input until every file has passed structural, integrity, provenance, freshness, environment, and non-synthetic checks.

## Required controls

- Every evidence item has a unique identifier and declared type.
- The path is relative and resolves to a regular file; symbolic links are rejected.
- The declared SHA-256 digest must match the file bytes.
- Producer and source must be allowlisted by policy.
- The evidence must identify the production environment.
- Issued and expiry timestamps must be timezone-aware, fresh, and internally valid.
- Evidence marked synthetic is rejected by the production policy.
- All required evidence types must be represented.

The intake result binds accepted entries into one deterministic `evidence_set_digest`. Later promotion decisions bind to that digest rather than trusting mutable filenames.

## Required external types

The default policy requires real evidence for container runtime certification, hosted CI, provider effects, and an independent security review. Replace the example specification with actual evidence and real SHA-256 values.

```bash
python3 tools/futurediff_promotion.py evidence-intake \
  --root /secure/evidence \
  --specification external-evidence-specification.json \
  --policy config/external-evidence-policy.json \
  --output dist/promotion/external-evidence-intake.json
```

A failed or incomplete intake exits non-zero and cannot be promoted.
