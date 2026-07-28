#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FILE="${1:?usage: verify-signature.sh FILE PUBLIC_KEY SIGNATURE}"
PUBLIC_KEY="${2:?usage: verify-signature.sh FILE PUBLIC_KEY SIGNATURE}"
SIGNATURE="${3:?usage: verify-signature.sh FILE PUBLIC_KEY SIGNATURE}"
exec python3 "$ROOT/tools/futurediff_assurance.py" verify-signature --file "$FILE" --public-key "$PUBLIC_KEY" --signature "$SIGNATURE"
