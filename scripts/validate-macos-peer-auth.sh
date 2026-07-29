#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: this validation must run on macOS" >&2
  exit 2
fi

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl with Unix-socket support is required" >&2
  exit 2
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required to validate the health response" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/futurediff-peer-auth.XXXXXX")"
daemon_pid=""

cleanup() {
  if [[ -n "$daemon_pid" ]] && kill -0 "$daemon_pid" 2>/dev/null; then
    kill -TERM "$daemon_pid" 2>/dev/null || true
    wait "$daemon_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp_root"
}
trap cleanup EXIT INT TERM

cd "$repo_root"

export CGO_ENABLED=1

echo "==> Testing peer-auth package"
go test ./internal/peerauth
go test -race ./internal/peerauth

echo "==> Building authenticated daemon"
go build -trimpath -o "$tmp_root/futurediffd" ./cmd/futurediffd

root="$tmp_root/root"
socket="$root/futurediff.sock"
log_file="$tmp_root/futurediffd.log"

mkdir -p "$root"
chmod 700 "$root"

"$tmp_root/futurediffd" \
  --root "$root" \
  --socket "$socket" \
  >"$log_file" 2>&1 &
daemon_pid=$!

for _ in {1..100}; do
  if [[ -S "$socket" ]]; then
    break
  fi
  if ! kill -0 "$daemon_pid" 2>/dev/null; then
    echo "error: daemon exited before creating its socket" >&2
    cat "$log_file" >&2
    exit 1
  fi
  sleep 0.1
done

if [[ ! -S "$socket" ]]; then
  echo "error: daemon socket was not created" >&2
  cat "$log_file" >&2
  exit 1
fi

echo "==> Calling authenticated health endpoint"
health_json="$(curl --fail --silent --show-error \
  --unix-socket "$socket" \
  http://localhost/v1/health)"

python3 - "$health_json" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
if payload.get("status") != "ok":
    raise SystemExit(f"unexpected health status: {payload.get('status')!r}")
peer_auth = payload.get("peer_auth") or {}
if peer_auth.get("required") is not True:
    raise SystemExit(f"peer authentication was not required: {peer_auth!r}")
if int(peer_auth.get("allowed_uid_count", 0)) < 1:
    raise SystemExit(f"no allowed peer UID was configured: {peer_auth!r}")
print("authenticated macOS peer check: PASS")
PY

echo "==> macOS peer-auth validation passed"
