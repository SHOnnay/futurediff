#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
METRICS="${1:?usage: slo-check.sh METRICS_JSON [OUTPUT_JSON]}"
OUTPUT="${2:-}"
ARGS=(slo-evaluate --metrics "$METRICS" --policy "$ROOT/config/slo-policy.json")
if [[ -n "$OUTPUT" ]]; then ARGS+=(--output "$OUTPUT"); fi
exec python3 "$ROOT/tools/futurediff_assurance.py" "${ARGS[@]}"
