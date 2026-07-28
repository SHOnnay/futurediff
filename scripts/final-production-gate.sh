#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
./scripts/operations-assurance.sh dist/operations
python3 - <<'PY'
import json
from pathlib import Path
p = Path('dist/operations/local-production-gate.json')
doc = json.loads(p.read_text(encoding='utf-8'))
if doc.get('passed') is not True:
    raise SystemExit('local production gate failed')
if doc.get('external_certification_required') is not True:
    raise SystemExit('external certification disclaimer missing')
print(doc['gate_digest'])
PY
