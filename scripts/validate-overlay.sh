#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "== python compile =="
python3 -m py_compile \
  tools/futurediff_assurance.py \
  tools/futurediff_operations.py \
  tools/futurediff_promotion.py \
  tests/test_assurance.py \
  tests/test_operations.py \
  tests/test_promotion.py

echo "== unit tests =="
python3 -m unittest discover -s tests -p 'test_*.py' -v

echo "== shell syntax =="
for f in scripts/*.sh; do bash -n "$f"; done

echo "== JSON parse =="
python3 - <<'PY'
import json
from pathlib import Path
for p in sorted(Path('.').rglob('*.json')):
    if '__pycache__' in p.parts or 'dist' in p.parts:
        continue
    json.loads(p.read_text(encoding='utf-8'))
    print(p)
PY

echo "== workflow YAML parse =="
python3 - <<'PY'
from pathlib import Path
import yaml
for p in sorted(Path('.github/workflows').glob('*.yml')):
    yaml.safe_load(p.read_text(encoding='utf-8'))
    print(p)
PY

echo "== strict production policy rejects example evidence =="
if python3 tools/futurediff_promotion.py evidence-intake \
  --root examples \
  --specification examples/external-evidence-specification.example.json \
  --policy config/external-evidence-policy.json \
  --now 2026-07-28T12:00:00Z \
  --output /tmp/futurediff-example-evidence.json; then
  echo "example evidence unexpectedly passed" >&2
  exit 1
else
  echo "expected blocked result"
fi

echo "== secret scan =="
python3 tools/futurediff_assurance.py secret-scan --root . --output /tmp/futurediff-secret-scan.json

echo "== license policy =="
python3 tools/futurediff_assurance.py license-scan --root . --policy config/license-policy.json --output /tmp/futurediff-license-scan.json

echo "== SLO policy =="
python3 tools/futurediff_assurance.py slo-evaluate --metrics examples/slo-metrics.example.json --policy config/slo-policy.json --output /tmp/futurediff-slo.json

echo "== recovery drill =="
python3 tools/futurediff_assurance.py recovery-drill --output /tmp/futurediff-recovery.json

echo "== chaos checks =="
python3 tools/futurediff_assurance.py chaos-run --output /tmp/futurediff-chaos.json

echo "== readiness =="
python3 tools/futurediff_assurance.py readiness --root . --policy config/production-readiness-policy.json --output /tmp/futurediff-readiness.json

echo "== operational assurance =="
./scripts/operations-assurance.sh dist/operations

echo "== package manifest =="
sha256sum -c MANIFEST.sha256

echo "VALIDATION PASS"
