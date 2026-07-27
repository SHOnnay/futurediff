#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
if [[ -x bin/futurediff-demo ]]; then
  exec bin/futurediff-demo "$@"
fi
exec go run ./cmd/futurediff-demo "$@"
