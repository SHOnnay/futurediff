#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <subject-file> <owner/repository> <output-json>" >&2
  exit 2
fi

SUBJECT=$1
REPOSITORY=$2
OUTPUT=$3

if [[ ! -f "$SUBJECT" || -L "$SUBJECT" ]]; then
  echo '{"kind":"github_artifact_attestation","passed":false,"reason":"subject_unavailable"}' >&2
  exit 2
fi
if [[ ! "$REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
  echo '{"kind":"github_artifact_attestation","passed":false,"reason":"invalid_repository"}' >&2
  exit 2
fi
if ! command -v gh >/dev/null 2>&1; then
  echo '{"kind":"github_artifact_attestation","passed":false,"reason":"gh_cli_unavailable"}' >&2
  exit 2
fi

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if ! gh attestation verify "$SUBJECT" --repo "$REPOSITORY" --format json >"$TMP"; then
  python3 - "$SUBJECT" "$REPOSITORY" "$OUTPUT" <<'PY'
import hashlib, json, pathlib, sys
subject = pathlib.Path(sys.argv[1])
repository = sys.argv[2]
output = pathlib.Path(sys.argv[3])
doc = {
    "format_version": "1.0",
    "kind": "github_artifact_attestation",
    "passed": False,
    "repository": repository,
    "subject": subject.name,
    "subject_sha256": hashlib.sha256(subject.read_bytes()).hexdigest(),
    "reason": "verification_failed",
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
PY
  exit 2
fi

python3 - "$SUBJECT" "$REPOSITORY" "$TMP" "$OUTPUT" <<'PY'
import hashlib, json, pathlib, sys
subject = pathlib.Path(sys.argv[1])
repository = sys.argv[2]
raw = pathlib.Path(sys.argv[3]).read_bytes()
output = pathlib.Path(sys.argv[4])
doc = {
    "format_version": "1.0",
    "kind": "github_artifact_attestation",
    "passed": True,
    "repository": repository,
    "subject": subject.name,
    "subject_sha256": hashlib.sha256(subject.read_bytes()).hexdigest(),
    "verification_output_sha256": hashlib.sha256(raw).hexdigest(),
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(doc, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8")
print(json.dumps(doc, sort_keys=True, indent=2))
PY
