#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -lt 6 ]]; then
  echo "usage: $0 <evidence-root> <evidence-spec> <claims-json> <candidate-json> <approvals-json> <output-dir> [exception-result ...]" >&2
  exit 2
fi

EVIDENCE_ROOT=$1
SPEC=$2
CLAIMS=$3
CANDIDATE=$4
APPROVALS=$5
OUTPUT=$6
shift 6
EXCEPTIONS=("$@")
mkdir -p "$OUTPUT"
NOW=${FUTUREDIFF_EVALUATED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}

python3 "$ROOT/tools/futurediff_promotion.py" evidence-intake \
  --root "$EVIDENCE_ROOT" \
  --specification "$SPEC" \
  --policy "$ROOT/config/external-evidence-policy.json" \
  --now "$NOW" \
  --output "$OUTPUT/external-evidence-intake.json"

python3 "$ROOT/tools/futurediff_promotion.py" oidc-claims-verify \
  --claims "$CLAIMS" \
  --policy "$ROOT/config/hosted-identity-policy.json" \
  --now "$NOW" \
  --output "$OUTPUT/hosted-identity.json"

ARGS=()
for exception in "${EXCEPTIONS[@]}"; do
  ARGS+=(--exception "$exception")
done
python3 "$ROOT/tools/futurediff_promotion.py" promotion-evaluate \
  --candidate "$CANDIDATE" \
  --intake "$OUTPUT/external-evidence-intake.json" \
  --identity "$OUTPUT/hosted-identity.json" \
  --approvals "$APPROVALS" \
  --policy "$ROOT/config/promotion-policy.json" \
  "${ARGS[@]}" \
  --output "$OUTPUT/promotion-decision.json"

python3 - "$OUTPUT/promotion-decision.json" "$NOW" "$OUTPUT/transparency-record.json" <<'PY'
import json, pathlib, sys
promotion = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
record = {"recorded_at": sys.argv[2], "payload": promotion}
pathlib.Path(sys.argv[3]).write_text(json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY

python3 "$ROOT/tools/futurediff_promotion.py" transparency-append \
  --ledger "$OUTPUT/transparency-ledger.json" \
  --record "$OUTPUT/transparency-record.json" \
  --output "$OUTPUT/transparency-ledger.json"

python3 "$ROOT/tools/futurediff_promotion.py" release-metadata \
  --candidate "$CANDIDATE" \
  --promotion "$OUTPUT/promotion-decision.json" \
  --ledger "$OUTPUT/transparency-ledger.json" \
  --output "$OUTPUT/github-release-metadata.json"

cat > "$OUTPUT/promotion-bundle-specification.json" <<'JSON'
{"artifacts":[{"id":"external-evidence-intake","path":"external-evidence-intake.json"},{"id":"hosted-identity","path":"hosted-identity.json"},{"id":"promotion-decision","path":"promotion-decision.json"},{"id":"transparency-ledger","path":"transparency-ledger.json"},{"id":"github-release-metadata","path":"github-release-metadata.json"}]}
JSON

python3 "$ROOT/tools/futurediff_promotion.py" bundle-build \
  --root "$OUTPUT" \
  --specification "$OUTPUT/promotion-bundle-specification.json" \
  --archive "$OUTPUT/FutureDiff-production-promotion.zip" \
  --prefix "FutureDiff-production-promotion" \
  --output "$OUTPUT/promotion-bundle-verification.json"

sha256sum "$OUTPUT/FutureDiff-production-promotion.zip" > "$OUTPUT/FutureDiff-production-promotion.zip.sha256"
echo "RELEASE PROMOTION PASS"
