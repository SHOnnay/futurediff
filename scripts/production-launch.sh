#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -ne 5 ]]; then
  echo "usage: $0 <promotion-decision> <postdeploy-evidence> <rollback-evidence> <launch-policy> <output-dir>" >&2
  exit 2
fi

PROMOTION=$1
POSTDEPLOY_INPUT=$2
ROLLBACK_INPUT=$3
LAUNCH_POLICY=$4
OUTPUT=$5
mkdir -p "$OUTPUT"
NOW=${FUTUREDIFF_EVALUATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}

python3 "$ROOT/tools/futurediff_promotion.py" postdeploy-evaluate \
  --input "$POSTDEPLOY_INPUT" \
  --policy "$ROOT/config/postdeploy-policy.json" \
  --output "$OUTPUT/postdeploy-health.json"

python3 "$ROOT/tools/futurediff_promotion.py" rollback-evaluate \
  --input "$ROLLBACK_INPUT" \
  --policy "$ROOT/config/rollback-decision-policy.json" \
  --now "$NOW" \
  --output "$OUTPUT/rollback-decision.json"

python3 "$ROOT/tools/futurediff_promotion.py" launch-checklist \
  --promotion "$PROMOTION" \
  --postdeploy "$OUTPUT/postdeploy-health.json" \
  --rollback "$OUTPUT/rollback-decision.json" \
  --policy "$LAUNCH_POLICY" \
  --output "$OUTPUT/production-launch.json"

python3 - "$OUTPUT/production-launch.json" "$NOW" "$OUTPUT/launch-transparency-record.json" <<'PY'
import json, pathlib, sys
launch = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
record = {"recorded_at": sys.argv[2], "payload": launch}
pathlib.Path(sys.argv[3]).write_text(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY

python3 "$ROOT/tools/futurediff_promotion.py" transparency-append \
  --ledger "$OUTPUT/transparency-ledger.json" \
  --record "$OUTPUT/launch-transparency-record.json" \
  --output "$OUTPUT/transparency-ledger.json"

echo "PRODUCTION LAUNCH PASS"
