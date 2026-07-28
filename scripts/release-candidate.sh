#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-v1.40.0-overlay}"
OUT="${2:-$ROOT/dist}"
rm -rf "$OUT"
python3 "$ROOT/tools/futurediff_assurance.py" release-candidate \
  --root "$ROOT" \
  --output-dir "$OUT" \
  --name FutureDiff \
  --version "$VERSION" \
  --policy "$ROOT/config/production-readiness-policy.json"
