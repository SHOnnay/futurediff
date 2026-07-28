#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 4 ]]; then
  echo "usage: $0 CANONICAL_REPOSITORY BASE_ARCHIVE ARCHIVE_DIRECTORY OUTPUT_DIRECTORY" >&2
  exit 2
fi
repo=$1
base=$2
archives=$3
out=$4
root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$out"
run_allow_blocked() {
  set +e
  "$@"
  code=$?
  set -e
  if [[ $code -gt 1 ]]; then exit "$code"; fi
}
run_allow_blocked python3 "$root/tools/futurediff_closure.py" integration-receipt \
  --repository "$repo" --base-archive "$base" --manifest "$root/MANIFEST.sha256" \
  --output "$out/integration-receipt.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" archive-catalog \
  --root "$archives" --expected "$root/examples/archive-expectations.example.json" \
  --output "$out/archive-catalog.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" freshness-plan \
  --spec "$root/examples/evidence-freshness.example.json" --policy "$root/config/evidence-freshness-policy.json" \
  --output "$out/evidence-freshness.json"
python3 "$root/tools/futurediff_closure.py" certification-campaign \
  --spec "$root/examples/certification-campaign.example.json" --output "$out/certification-campaign.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" security-review \
  --review "$root/examples/security-review.example.json" --policy "$root/config/security-review-policy.json" \
  --output "$out/security-review.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" load-soak \
  --evidence "$root/examples/load-soak.example.json" --policy "$root/config/load-soak-policy.json" \
  --output "$out/load-soak.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" dr-evidence \
  --evidence "$root/examples/dr-evidence.example.json" --policy "$root/config/dr-evidence-policy.json" \
  --output "$out/dr-evidence.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" change-control \
  --record "$root/examples/change-control.example.json" --policy "$root/config/change-control-policy.json" \
  --output "$out/change-control.json"
python3 "$root/tools/futurediff_closure.py" credential-readiness \
  --record "$root/examples/credential-readiness.example.json" --policy "$root/config/credential-readiness-policy.json" \
  --output "$out/credential-readiness.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" smoke-test \
  --record "$root/examples/smoke-test.example.json" --policy "$root/config/smoke-test-policy.json" \
  --output "$out/smoke-test.json"
run_allow_blocked python3 "$root/tools/futurediff_closure.py" rollback-exercise \
  --record "$root/examples/rollback-exercise.example.json" --policy "$root/config/rollback-exercise-policy.json" \
  --output "$out/rollback-exercise.json"
python3 "$root/tools/futurediff_closure.py" operational-signoff \
  --record "$root/examples/operational-signoff.example.json" --policy "$root/config/operational-signoff-policy.json" \
  --output "$out/operational-signoff.json"
args=()
for f in integration-receipt archive-catalog evidence-freshness certification-campaign security-review load-soak dr-evidence change-control credential-readiness smoke-test rollback-exercise operational-signoff; do
  args+=(--result "$out/$f.json")
done
for k in canonical-integration-receipt historical-archive-catalog evidence-freshness-plan external-certification-campaign independent-security-review measured-load-soak-evidence disaster-recovery-evidence change-freeze-control production-credential-readiness deployment-smoke-test rollback-exercise operational-signoff; do
  args+=(--required-kind "$k")
done
run_allow_blocked python3 "$root/tools/futurediff_closure.py" completion-decision "${args[@]}" --output "$out/production-completion-decision.json"
files=()
for f in "$out"/*.json; do files+=(--file "$(basename "$f")"); done
python3 "$root/tools/futurediff_closure.py" bundle --root "$out" "${files[@]}" --output "$out/FutureDiff-production-closure-evidence.zip"
python3 "$root/tools/futurediff_closure.py" verify-bundle --bundle "$out/FutureDiff-production-closure-evidence.zip" --output "$out/closure-bundle-verification.json"
