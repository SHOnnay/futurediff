#!/usr/bin/env bash
set -euo pipefail

RUNTIME="${FUTUREDIFF_RUNTIME:-docker}"
RUNTIME_BINARY="${FUTUREDIFF_RUNTIME_BINARY:-}"
IMAGE="${FUTUREDIFF_TEST_IMAGE:-}"

if [[ -z "$IMAGE" || ! "$IMAGE" =~ @sha256:[0-9a-fA-F]{64}$ ]]; then
  echo "FUTUREDIFF_TEST_IMAGE must be name@sha256:<64 hex>" >&2
  exit 2
fi

root_dir="$(mktemp -d)"
daemon_pid=""
cleanup() {
  if [[ -n "$daemon_pid" ]]; then kill "$daemon_pid" 2>/dev/null || true; wait "$daemon_pid" 2>/dev/null || true; fi
  rm -rf "$root_dir"
}
trap cleanup EXIT

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
go build -o "$root_dir/futurediff" ./cmd/futurediff
go build -o "$root_dir/futurediffd" ./cmd/futurediffd

socket="$root_dir/futurediff.sock"
daemon_args=(--root "$root_dir/data" --socket "$socket" --runtime "$RUNTIME" --runtime-image "$IMAGE")
if [[ -n "$RUNTIME_BINARY" ]]; then daemon_args+=(--runtime-binary "$RUNTIME_BINARY"); fi
"$root_dir/futurediffd" "${daemon_args[@]}" >"$root_dir/daemon.log" 2>&1 &
daemon_pid=$!

for _ in $(seq 1 100); do
  if "$root_dir/futurediff" --socket "$socket" health >"$root_dir/health.json" 2>/dev/null; then break; fi
  sleep 0.1
done
python3 - "$root_dir/health.json" <<'PY'
import json,sys
h=json.load(open(sys.argv[1]))
assert h["status"] == "ok", h
assert h["oci"]["enforced_ready"] is True, h
assert h["oci"]["runtime"]["rootless"] is True, h
PY

repo="$root_dir/repo"
mkdir -p "$repo"
git -C "$repo" init -q -b main
git -C "$repo" config user.email certification@example.invalid
git -C "$repo" config user.name "FutureDiff Certification"
printf 'current\n' > "$repo/README.md"
git -C "$repo" add README.md
git -C "$repo" commit -qm base

"$root_dir/futurediff" --socket "$socket" create "$repo" enforced >"$root_dir/create.json"
tx="$(python3 - "$root_dir/create.json" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))["transaction"]["transaction_id"])
PY
)"
"$root_dir/futurediff" --socket "$socket" execute "$tx" /bin/sh -c "test ! -e .git; printf 'future\\n' > README.md" >"$root_dir/execute.json"
[[ "$(cat "$repo/README.md")" == "current" ]]
python3 - "$root_dir/execute.json" <<'PY'
import json,sys,os
v=json.load(open(sys.argv[1]))
e=v["execution"]
assert e["workspace_synchronized"] is True, e
assert e["runtime_kind"] in {"docker","podman"}, e
assert e["termination_reason"] == "exited", e
assert os.path.exists(e["evidence_path"]), e
PY

"$root_dir/futurediff" --socket "$socket" seal "$tx" >"$root_dir/seal.json"
cat >"$root_dir/verify.json" <<'JSON'
{
  "format_version": "0.1",
  "contract_id": "rootless-certification",
  "policy_version": "policy-0.1",
  "checks": [
    {
      "check_id": "readme",
      "required": true,
      "executor": "oci_command",
      "type": "command",
      "command": ["/bin/sh", "-c", "grep -q future README.md"],
      "timeout_seconds": 30
    }
  ]
}
JSON
"$root_dir/futurediff" --socket "$socket" verify "$tx" "$root_dir/verify.json" >"$root_dir/verified.json"
digest="$("$root_dir/futurediff" --socket "$socket" approval-material "$tx" | python3 -c 'import json,sys; print(json.load(sys.stdin)["transaction_digest"])')"
"$root_dir/futurediff" --socket "$socket" approve "$tx" "$digest" >/dev/null
"$root_dir/futurediff" --socket "$socket" commit "$tx" "$digest" >"$root_dir/committed.json"
[[ "$(cat "$repo/README.md")" == "current" ]]
[[ "$(git -C "$repo" show "refs/heads/futurediff/$tx:README.md")" == "future" ]]

echo "ROOTLESS_OCI_CERTIFICATION=PASS"
