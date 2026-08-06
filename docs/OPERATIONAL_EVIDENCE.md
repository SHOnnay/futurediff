# Operational Evidence

`./scripts/operations-assurance.sh` generates hash-bound JSON evidence, a catalog, a deterministic ZIP, independent ZIP verification, and a final local production gate.

The gate scope is deliberately labeled `local-operational-assurance-only` and always states that external certification is still required. Synthetic examples cannot be presented as real provider, runtime, hosted-platform, or production-load certification.

## Corruption/Lock/Disk-Pressure Certification

The resilience certification drill (`scripts/certify-corruption-lock-disk-pressure.sh`) produces hash-bound, secret-free operational evidence under `docs/certification/corruption-lock-disk-pressure-<timestamp>/`:

- **SUMMARY.json**: `kind: "corruption-lock-disk-pressure-certification"`, host `Darwin-arm64`, failures 0, 33 evidence rows (29 `real_local`, 4 `deterministic_injection`), scenarios A–J
- **Per-scenario JSON**: each records `reason_code`, `component`, `path_class`, `safe_to_retry`, `automatic_cleanup_allowed`, `backup_available`, `backup_verified`, `recovery_required`, `recommended_action`
- **No secrets**: `secrets-scan.txt` confirms zero credential/token/path/env leakage in evidence
- **MANIFEST.sha256** entries include all certification artifacts