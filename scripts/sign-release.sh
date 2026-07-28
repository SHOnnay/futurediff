#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FILE="${1:?usage: sign-release.sh FILE PRIVATE_KEY SIGNATURE}"
PRIVATE_KEY="${2:?usage: sign-release.sh FILE PRIVATE_KEY SIGNATURE}"
SIGNATURE="${3:?usage: sign-release.sh FILE PRIVATE_KEY SIGNATURE}"
exec python3 "$ROOT/tools/futurediff_assurance.py" sign --file "$FILE" --private-key "$PRIVATE_KEY" --signature "$SIGNATURE"
