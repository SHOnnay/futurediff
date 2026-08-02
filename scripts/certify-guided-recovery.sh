#!/usr/bin/env bash
#
# certify-guided-recovery.sh
#
# Local guided-recovery certification for the FutureDiff guided CLI. Exercises
# the fdif recover contract and the hardened current-transaction selection
# store end-to-end against a real daemon, without any external provider:
#
#   A. Stale selection  -> fdif recover --json reports selection_transaction_missing;
#      fdif recover --yes clears the stale pointer.
#   B. Deleted active workspace -> fdif recover --json reports workspace_missing
#      and recommends fdif abort --yes; no workspace is ever recreated by the
#      guided CLI.
#   C. Interrupted sealed flow -> fdif recover reports no_recovery_needed; the
#      sealed material stays durable in the ledger and finish completes it.
#   D. Interrupted publication -> recovery-drill planner refuses blind retry on
#      ambiguous provider state and re-arms when the provider proves no
#      mutation; the canonical daemon refuses a second recover on an already
#      committed change (no double publish).
#
# Requires: go, git, jq, python3. No network access and no tokens.
#
# Evidence is written under docs/certification/guided-recovery-<timestamp>/.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 2; }
}
require go
require git
require jq
require python3

stamp="$(date -u +%Y%m%d-%H%M%S)"
# Keep the runtime home short: the daemon binds a Unix socket inside it, and
# macOS/Linux reject socket paths over ~104 bytes.
cert_root="$(mktemp -d /tmp/futurediff-guided-recovery.XXXXXX)"
evidence_dir="$repo_root/docs/certification/guided-recovery-$stamp"
mkdir -p "$evidence_dir"
echo "certification working directory: $cert_root"
echo "evidence directory: $evidence_dir"

# --- 0. Build binaries -----------------------------------------------------
echo ">> building binaries"
(cd "$repo_root" && go build -o "$cert_root/futurediff" ./cmd/futurediff)
(cd "$repo_root" && go build -o "$cert_root/futurediffd" ./cmd/futurediffd)
(cd "$repo_root" && go build -o "$cert_root/fdif" ./cmd/fdif)
(cd "$repo_root" && go build -o "$cert_root/futurediff-recovery-drill" ./cmd/futurediff-recovery-drill)

# Isolated runtime home: daemon data, selection store and workspaces all live
# here, so the drill never touches the real ~/.futurediff.
export FDIF_HOME="$cert_root/home"
export PATH="$cert_root:$PATH"

# Source repository the safe workspaces are linked from.
source_repo="$cert_root/source"
mkdir -p "$source_repo"
git -C "$source_repo" init -q -b main
git -C "$source_repo" config user.name "Certification"
git -C "$source_repo" config user.email "cert@localhost"
printf 'hello\n' > "$source_repo/README.md"
git -C "$source_repo" add README.md
git -C "$source_repo" commit -q -m init

echo ">> starting daemon"
fdif daemon start >/dev/null
trap 'fdif daemon stop >/dev/null 2>&1 || true' EXIT
fdif doctor --json > "$evidence_dir/00-doctor.json" 2>/dev/null || true

fail_count=0
record() {
  # record <name> <jq-expression> <expected> <actual-json-file>
  local name="$1" expr="$2" expected="$3" file="$4"
  local actual
  actual="$(jq -r "$expr" "$file")"
  if [[ "$actual" == "$expected" ]]; then
    echo "  PASS $name ($actual)"
  else
    echo "  FAIL $name: expected $expected, got $actual" >&2
    fail_count=$((fail_count + 1))
  fi
}

# --- A. Stale selection ------------------------------------------------------
echo ">> A. stale selection"
(cd "$source_repo" && fdif start --yes >/dev/null 2>&1)
selected_id="$(jq -r .transaction_id "$FDIF_HOME/current-transaction.json" 2>/dev/null || true)"
if [[ -z "$selected_id" || "$selected_id" == "null" ]]; then
  # Selection file may live at the resolved state path; locate it.
  selected_id="$(jq -r .transaction_id "$(find "$FDIF_HOME" -name current-transaction.json | head -1)")"
fi
echo "  selected: $selected_id"
# Point the selection at a transaction the daemon never knew.
cat > "$(find "$FDIF_HOME" -name current-transaction.json | head -1)" <<EOF
{"transaction_id":"tx_deadbeefdeadbeefdeadbeef","repository_root":"$source_repo","selected_at":"2026-08-02T00:00:00Z"}
EOF
(cd "$source_repo" && fdif recover --json) > "$evidence_dir/a1-stale-report.json"
record "a1 stale selection reason" ".reason_code" "selection_transaction_missing" "$evidence_dir/a1-stale-report.json"
record "a1 stale selection not repaired" ".selection_repaired" "false" "$evidence_dir/a1-stale-report.json"
(cd "$source_repo" && fdif recover --yes --json) > "$evidence_dir/a2-stale-repair.json"
record "a2 stale selection repaired" ".selection_repaired" "true" "$evidence_dir/a2-stale-repair.json"
# The stale pointer must be gone (no transaction_id in the selection file).
remaining="$(jq -r .transaction_id "$(find "$FDIF_HOME" -name current-transaction.json | head -1)" 2>/dev/null || echo cleared)"
if [[ "$remaining" == "cleared" ]]; then
  echo "  PASS a3 stale pointer cleared"
else
  echo "  FAIL a3 stale pointer not cleared: $remaining" >&2
  fail_count=$((fail_count + 1))
fi
# Re-select the real change so later scenarios start from a known state.
real_id="$(cd "$source_repo" && fdif transactions --json | jq -r '.transactions[0].transaction_id' 2>/dev/null || true)"
if [[ -z "$real_id" || "$real_id" == "null" ]]; then
  real_id="$(cd "$source_repo" && fdif list --json 2>/dev/null | jq -r '.transactions[0].transaction_id' || true)"
fi
echo "  re-selecting real change: $real_id"
(cd "$source_repo" && fdif use "$real_id" >/dev/null 2>&1)

# --- B. Deleted active workspace ---------------------------------------------
echo ">> B. deleted active workspace"
workspace_path="$(cd "$source_repo" && fdif workspace 2>/dev/null | tail -1)"
echo "  workspace: $workspace_path"
printf 'an edit\n' >> "$workspace_path/README.md"
rm -rf "$workspace_path"
(cd "$source_repo" && fdif recover --json) > "$evidence_dir/b1-workspace-missing.json"
record "b1 workspace missing reason" ".reason_code" "workspace_missing" "$evidence_dir/b1-workspace-missing.json"
record "b1 workspace unavailable" ".workspace_available" "false" "$evidence_dir/b1-workspace-missing.json"
record "b1 recommends abort" ".recommended_action | endswith(\"fdif abort $real_id --yes\")" "true" "$evidence_dir/b1-workspace-missing.json"
if [[ -e "$workspace_path" ]]; then
  echo "  FAIL b2 guided CLI recreated the workspace" >&2
  fail_count=$((fail_count + 1))
else
  echo "  PASS b2 workspace not recreated"
fi
(cd "$source_repo" && fdif abort "$real_id" --yes --json) > "$evidence_dir/b2-abort.json" 2>/dev/null || \
  (cd "$source_repo" && fdif abort --yes --json) > "$evidence_dir/b2-abort.json"
record "b2 abort completes" ".transaction.status" "aborted" "$evidence_dir/b2-abort.json"

# --- C. Interrupted sealed flow ----------------------------------------------
echo ">> C. interrupted sealed flow"
(cd "$source_repo" && fdif start --yes >/dev/null 2>&1)
sealed_id="$(jq -r .transaction_id "$(find "$FDIF_HOME" -name current-transaction.json | head -1)")"
# The interrupted flow froze a non-empty reviewed change.
sealed_ws="$(cd "$source_repo" && fdif workspace | tail -1)"
printf 'sealed change\n' >> "$sealed_ws/README.md"
(cd "$source_repo" && fdif seal --yes >/dev/null 2>&1)
(cd "$source_repo" && fdif recover --json) > "$evidence_dir/c1-sealed-recovery.json"
record "c1 sealed no recovery needed" ".reason_code" "no_recovery_needed" "$evidence_dir/c1-sealed-recovery.json"
record "c1 status is sealed" ".current_status" "sealed" "$evidence_dir/c1-sealed-recovery.json"
record "c1 workspace available" ".workspace_available" "true" "$evidence_dir/c1-sealed-recovery.json"
# The sealed material must still be publishable after the interrupted flow.
(cd "$source_repo" && fdif finish "$sealed_id" --yes >/dev/null 2>&1)
final_status="$(cd "$source_repo" && fdif get "$sealed_id" --json 2>/dev/null | jq -r .transaction.status || true)"
if [[ "$final_status" == "committed" ]]; then
  echo "  PASS c2 sealed flow completed after recovery check"
else
  echo "  FAIL c2 sealed flow did not complete: status=$final_status" >&2
  fail_count=$((fail_count + 1))
fi

# --- D. Interrupted publication: planner + canonical refusal -----------------
echo ">> D. interrupted publication"
cat > "$cert_root/drill-ambiguous.json" <<EOF
{
  "name": "guided-interrupted-publication-ambiguous",
  "transaction_status": "needs_reconciliation",
  "effect_status": "unknown",
  "provider_status": "unknown"
}
EOF
cat > "$cert_root/drill-not-committed.json" <<EOF
{
  "name": "guided-interrupted-publication-not-committed",
  "transaction_status": "needs_reconciliation",
  "effect_status": "unknown",
  "provider_status": "not_committed"
}
EOF
futurediff-recovery-drill --input "$cert_root/drill-ambiguous.json" > "$evidence_dir/d1-planner-ambiguous.json"
record "d1 ambiguous refuses blind retry" ".plan.blind_retry_allowed" "false" "$evidence_dir/d1-planner-ambiguous.json"
record "d1 ambiguous requires query" ".plan.action" "query_status" "$evidence_dir/d1-planner-ambiguous.json"
futurediff-recovery-drill --input "$cert_root/drill-not-committed.json" > "$evidence_dir/d2-planner-not-committed.json"
record "d2 not-committed re-arms effect" ".plan.action" "rearm_effect" "$evidence_dir/d2-planner-not-committed.json"
# Canonical idempotency: recover on an already committed change must be
# refused by the daemon, proving recovery never double-publishes.
if [[ "${final_status:-}" == "committed" ]]; then
  set +e
  raw="$(futurediff --socket "$FDIF_HOME/futurediff.sock" recover "$sealed_id" 2>&1)"
  rc=$?
  set -e
  echo "  recover-on-committed exit code: $rc"
  # The low-level client prints a single "error: ..." line to stderr; the
  # daemon response body is JSON. Preserve it as valid JSON evidence.
  body="$(printf '%s' "$raw" | sed -n 's/^error: //p')"
  if printf '%s' "$body" | jq -e . >/dev/null 2>&1; then
    jq --argjson exit_code "$rc" --arg raw "$raw" '{kind:"daemon-recover-refusal", exit_code:$exit_code, raw:$raw} + .' <<<"$body" > "$evidence_dir/d3-recover-committed.json"
  else
    jq -n --argjson exit_code "$rc" --arg raw "$raw" '{kind:"daemon-recover-refusal", exit_code:$exit_code, raw:$raw}' > "$evidence_dir/d3-recover-committed.json"
  fi
  if [[ "$rc" -eq 0 ]]; then
    echo "  FAIL d3 daemon accepted recover on committed change" >&2
    fail_count=$((fail_count + 1))
  else
    echo "  PASS d3 daemon refused recover on committed change"
  fi
fi

# --- Evidence summary --------------------------------------------------------
echo ">> evidence"
cat > "$evidence_dir/SUMMARY.json" <<EOF
{
  "kind": "guided-recovery-certification",
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "host": "$(uname -s)-$(uname -m)",
  "scenarios": ["stale_selection", "workspace_missing", "interrupted_sealed_flow", "interrupted_publication"],
  "failures": $fail_count,
  "evidence": $(find "$evidence_dir" -maxdepth 1 -type f -name '*.json' | sort | jq -R . | jq -s .)
}
EOF
echo "  summary: $evidence_dir/SUMMARY.json"
if [[ "$fail_count" -eq 0 ]]; then
  echo "CERTIFICATION PASSED"
  exit 0
fi
echo "CERTIFICATION FAILED ($fail_count checks)" >&2
exit 1
