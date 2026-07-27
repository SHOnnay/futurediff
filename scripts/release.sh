#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --verify HEAD 2>/dev/null || printf unknown)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
DIRTY="false"
if git status --porcelain 2>/dev/null | grep -q .; then DIRTY="true"; fi
OUT="${OUT:-dist/futurediff-${VERSION}-linux-amd64}"
rm -rf "$OUT"
mkdir -p "$OUT"
LDFLAGS="-s -w -X github.com/SHOnnay/futurediff/internal/buildinfo.Version=${VERSION} -X github.com/SHOnnay/futurediff/internal/buildinfo.Commit=${COMMIT} -X github.com/SHOnnay/futurediff/internal/buildinfo.Date=${DATE} -X github.com/SHOnnay/futurediff/internal/buildinfo.Dirty=${DIRTY}"
for cmd in futurediff futurediffd futurediff-mcp futurediff-certify futurediff-bench futurediff-sbom futurediff-admin futurediff-demo futurediff-integrate futurediff-provenance futurediff-cert-suite futurediff-install futurediff-platform futurediff-agent-bench futurediff-verify-release futurediff-provider-cert futurediff-audit futurediff-prune futurediff-doctor futurediff-api-contract futurediff-export futurediff-restore futurediff-replay futurediff-config-lint futurediff-api-diff futurediff-effectspec futurediff-policy-explain futurediff-recovery-drill futurediff-metrics futurediff-support-bundle futurediff-approval futurediff-policy-bundle futurediff-diff futurediff-upgrade-rehearsal futurediff-compat futurediff-maintenance futurediff-evidence futurediff-timeline futurediff-threat-test futurediff-config-snapshot futurediff-approval-quorum futurediff-incident futurediff-drain futurediff-operator-receipt futurediff-retention-policy futurediff-effect-graph futurediff-slo futurediff-readiness futurediff-secret-scan futurediff-quota futurediff-api-audit futurediff-daemon-lock futurediff-rate-policy futurediff-config-sign futurediff-root-audit futurediff-ledger-maintain futurediff-integrity-checkpoint futurediff-lease-cleanup futurediff-repository-policy futurediff-expire futurediff-idempotency-gc futurediff-storage-check futurediff-openapi futurediff-backup-catalog; do
  go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$cmd" "./cmd/$cmd"
done
cp README.md ARCHITECTURE.md "$OUT/"
"$OUT/futurediff-sbom" --root . --output "$OUT/futurediff.spdx.json" --files=true
PROV_ARGS=()
for artifact in "$OUT"/futurediff*; do
  if [[ -f "$artifact" && "$artifact" != *.json && "$artifact" != *SHA256SUMS ]]; then
    PROV_ARGS+=(--artifact "$artifact")
  fi
done
PROV_ARGS+=(--artifact "$OUT/futurediff.spdx.json")
"$OUT/futurediff-provenance" "${PROV_ARGS[@]}" --output "$OUT/futurediff.intoto.jsonl"   --builder-id "https://github.com/${GITHUB_WORKFLOW_REF:-local/futurediff-release}"   --invocation-id "${GITHUB_RUN_ID:-local-${DATE}}"   --source-uri "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-SHOnnay/futurediff}"   --source-digest "$COMMIT"
(
  cd "$OUT"
  sha256sum futurediff* > SHA256SUMS
)
tar -C "$(dirname "$OUT")" -czf "${OUT}.tar.gz" "$(basename "$OUT")"
sha256sum "${OUT}.tar.gz" > "${OUT}.tar.gz.sha256"
printf 'release=%s\ncommit=%s\noutput=%s\n' "$VERSION" "$COMMIT" "${OUT}.tar.gz"
