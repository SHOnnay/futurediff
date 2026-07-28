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
go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/" ./cmd/...
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
