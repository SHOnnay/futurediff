#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

: "${FUTUREDIFF_GITHUB_TOKEN:?FUTUREDIFF_GITHUB_TOKEN is required}"
: "${FUTUREDIFF_SLACK_TOKEN:?FUTUREDIFF_SLACK_TOKEN is required}"
: "${FUTUREDIFF_GITHUB_OWNER:?FUTUREDIFF_GITHUB_OWNER is required}"
: "${FUTUREDIFF_GITHUB_REPO:?FUTUREDIFF_GITHUB_REPO is required}"
: "${FUTUREDIFF_GITHUB_BASE:?FUTUREDIFF_GITHUB_BASE is required}"
: "${FUTUREDIFF_GITHUB_EXPECTED_SHA:?FUTUREDIFF_GITHUB_EXPECTED_SHA is required}"
: "${FUTUREDIFF_SLACK_CHANNEL:?FUTUREDIFF_SLACK_CHANNEL is required}"

GITHUB_BASE_URL="${FUTUREDIFF_GITHUB_BASE_URL:-https://api.github.com}"
SLACK_BASE_URL="${FUTUREDIFF_SLACK_BASE_URL:-https://slack.com/api}"
SLACK_TEXT="${FUTUREDIFF_SLACK_TEXT:-FutureDiff provider smoke benchmark}"
SLACK_THREAD_TS="${FUTUREDIFF_SLACK_THREAD_TS:-}"
ROOT_DIR="${FUTUREDIFF_PROVIDER_CERT_ROOT:-$(mktemp -d)}"
KEEP_ROOT="${FUTUREDIFF_PROVIDER_CERT_KEEP_ROOT:-false}"

cleanup() {
  if [[ "$KEEP_ROOT" != "true" ]]; then
    rm -rf "$ROOT_DIR"
  fi
}
trap cleanup EXIT

if [[ -x bin/futurediff-certify-providers ]]; then
  CERTIFY=(bin/futurediff-certify-providers)
else
  CERTIFY=(go run ./cmd/futurediff-certify-providers)
fi

REPORT_JSON="$ROOT_DIR/provider-certification.json"
REPORT_PACK="$ROOT_DIR/provider-certification.futurepack"
ARTIFACT_ROOT="$ROOT_DIR/artifacts"

"${CERTIFY[@]}" \
  --output "$REPORT_JSON" \
  --futurepack "$REPORT_PACK" \
  --root "$ARTIFACT_ROOT" \
  --github-base-url "$GITHUB_BASE_URL" \
  --github-owner "$FUTUREDIFF_GITHUB_OWNER" \
  --github-repo "$FUTUREDIFF_GITHUB_REPO" \
  --github-base "$FUTUREDIFF_GITHUB_BASE" \
  --github-expected-sha "$FUTUREDIFF_GITHUB_EXPECTED_SHA" \
  --slack-base-url "$SLACK_BASE_URL" \
  --slack-channel "$FUTUREDIFF_SLACK_CHANNEL" \
  --slack-text "$SLACK_TEXT" \
  --slack-thread-ts "$SLACK_THREAD_TS"

python3 - "$REPORT_JSON" <<'PY'
import json,sys
report=json.load(open(sys.argv[1]))
assert report["certified"] is True, report
checks={c["id"]: c for c in report["checks"]}
for key in ["github_branch_query", "github_stale_base_detected", "slack_prepare", "slack_post", "slack_recovery"]:
    assert checks[key]["status"] == "pass", checks[key]
PY

printf 'provider_certification_json=%s\nprovider_certification_futurepack=%s\nprovider_certification_root=%s\n' "$REPORT_JSON" "$REPORT_PACK" "$ARTIFACT_ROOT"
