#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
OUT="${1:-dist/operations}"
mkdir -p "$OUT"
TOOL=(python3 tools/futurediff_operations.py)

"${TOOL[@]}" deployment-validate \
  --input config/deployment-contract.json \
  --output "$OUT/deployment-contract.json"

"${TOOL[@]}" environment-parity \
  --input config/deployment-contract.json \
  --policy config/environment-parity-policy.json \
  --output "$OUT/environment-parity.json"

"${TOOL[@]}" compatibility-validate \
  --input examples/compatibility-matrix.example.json \
  --policy config/compatibility-policy.json \
  --output "$OUT/compatibility-matrix.json"

"${TOOL[@]}" upgrade-validate \
  --input examples/upgrade-plan.example.json \
  --output "$OUT/upgrade-plan.json"

"${TOOL[@]}" rollback-drill \
  --input examples/upgrade-plan.example.json \
  --output "$OUT/rollback-drill.json"

"${TOOL[@]}" capacity-evaluate \
  --input examples/capacity-test.example.json \
  --policy config/capacity-policy.json \
  --output "$OUT/capacity-test.json"

"${TOOL[@]}" soak-evaluate \
  --input examples/soak-test.example.json \
  --policy config/soak-policy.json \
  --output "$OUT/soak-test.json"

"${TOOL[@]}" observability-validate \
  --input config/observability-contract.json \
  --policy config/observability-policy.json \
  --output "$OUT/observability-contract.json"

"${TOOL[@]}" alert-routing-validate \
  --input examples/alert-routing.example.json \
  --policy config/alert-routing-policy.json \
  --output "$OUT/alert-routing.json"

"${TOOL[@]}" data-governance-validate \
  --input config/data-governance-policy.json \
  --output "$OUT/data-governance.json"

"${TOOL[@]}" incident-tabletop-evaluate \
  --input examples/incident-tabletop.example.json \
  --policy config/incident-tabletop-policy.json \
  --output "$OUT/incident-tabletop.json"

"${TOOL[@]}" approvals-validate \
  --input examples/release-approvals.example.json \
  --policy config/release-approval-policy.json \
  --output "$OUT/release-approvals.json"

python3 - "$OUT" <<'PY'
import json, sys
from pathlib import Path
out = Path(sys.argv[1])
paths = [
    "deployment-contract.json", "environment-parity.json", "compatibility-matrix.json",
    "upgrade-plan.json", "rollback-drill.json", "capacity-test.json", "soak-test.json",
    "observability-contract.json", "alert-routing.json", "data-governance.json",
    "incident-tabletop.json", "release-approvals.json",
]
spec = {"evidence": [{"id": p[:-5], "type": "operational-assurance", "path": str((out / p).as_posix()), "required": True} for p in paths]}
Path(out / "evidence-specification.json").write_text(json.dumps(spec, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY

"${TOOL[@]}" evidence-catalog \
  --root . \
  --specification "$OUT/evidence-specification.json" \
  --output "$OUT/evidence-catalog.json"

"${TOOL[@]}" bundle-build \
  --root . \
  --catalog "$OUT/evidence-catalog.json" \
  --archive "$OUT/FutureDiff-operational-evidence.zip" \
  --prefix "FutureDiff-operational-evidence" \
  --output "$OUT/certification-bundle.json"

"${TOOL[@]}" bundle-verify \
  --archive "$OUT/FutureDiff-operational-evidence.zip" \
  --output "$OUT/certification-bundle-verify.json"

python3 - "$OUT" <<'PY'
import json, sys
from pathlib import Path
out = Path(sys.argv[1])
paths = [
    "deployment-contract.json", "environment-parity.json", "compatibility-matrix.json",
    "upgrade-plan.json", "rollback-drill.json", "capacity-test.json", "soak-test.json",
    "observability-contract.json", "alert-routing.json", "data-governance.json",
    "incident-tabletop.json", "release-approvals.json", "evidence-catalog.json",
    "certification-bundle.json",
]
doc = {"artifact_paths": [str((out / p).as_posix()) for p in paths]}
Path(out / "final-gate-artifacts.json").write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY

"${TOOL[@]}" final-gate \
  --artifacts "$OUT/final-gate-artifacts.json" \
  --policy config/final-production-gate-policy.json \
  --output "$OUT/local-production-gate.json"

sha256sum "$OUT"/*.json "$OUT"/*.zip > "$OUT/SHA256SUMS.txt"
echo "OPERATIONAL ASSURANCE PASS"
