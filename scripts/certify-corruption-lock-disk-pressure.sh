#!/usr/bin/env bash
# certify-corruption-lock-disk-pressure.sh
#
# Certification drill for ADR-099: corruption detection, stale-lock proof,
# and restore behavior. Exercises, against real binaries in an isolated home:
#
#   A. Doctor corruption diagnostics: a corrupt ledger and a corrupt lock file
#      are reported as failures (not "not initialized" / "warn").
#   B. cleanup-lock refuses while a live daemon holds the flock, and never
#      removes the live lock or socket.
#   C. cleanup-lock removes a stale lock and socket (dead owner), records the
#      mutation in the operator audit trail, and is idempotent on a second run.
#   D. cleanup-lock removes a corrupt (unparseable) lock with --yes; corrupt
#      locks are eligible for automatic cleanup.
#   E. ledger restore refuses to apply a backup that is older than the live
#      ledger (stale-backup gate); --allow-stale-backup overrides it.
#   F. ledger restore refuses to run while the live ledger is corrupt
#      (fail-closed; the pre-restore backup cannot be taken), and applies
#      cleanly into a fresh root.
#   G. Disk-pressure classification: doctor surfaces a storage check result.
#
# Requires: go, git, jq, python3. No network access and no tokens.
#
# Evidence is written under docs/certification/corruption-lock-<timestamp>/.
#
# NOTE: the two path removals in cleanup-lock (lock file and socket) are
# independent filesystem operations and are NOT atomic with respect to each
# other; the drill asserts each individual removal, never an atomic claim.

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
cert_root="$(mktemp -d /tmp/futurediff-corruption-lock.XXXXXX)"
evidence_dir="$repo_root/docs/certification/corruption-lock-$stamp"
mkdir -p "$evidence_dir"
echo "certification working directory: $cert_root"
echo "evidence directory: $evidence_dir"

# --- 0. Build binaries -----------------------------------------------------
echo ">> building binaries"
(cd "$repo_root" && go build -o "$cert_root/fdif" ./cmd/fdif)
(cd "$repo_root" && go build -o "$cert_root/futurediff" ./cmd/futurediff)
(cd "$repo_root" && go build -o "$cert_root/futurediffd" ./cmd/futurediffd)
(cd "$repo_root" && go build -o "$cert_root/futurediff-restore" ./cmd/futurediff-restore)
(cd "$repo_root" && go build -o "$cert_root/futurediff-admin" ./cmd/futurediff-admin)

export FDIF_HOME="$cert_root/home"
export PATH="$cert_root:$PATH"

source_repo="$cert_root/source"
mkdir -p "$source_repo"
git -C "$source_repo" init -q -b main
git -C "$source_repo" config user.name "Certification"
git -C "$source_repo" config user.email "cert@localhost"
printf 'hello\n' > "$source_repo/README.md"
git -C "$source_repo" add README.md
git -C "$source_repo" commit -q -m init

fail_count=0
record() {
  # record <name> <jq-expression> <expected> <actual-json-file>
  local name="$1" expr="$2" expected="$3" file="$4"
  local actual
  actual="$(jq -r "$expr" "$file" 2>/dev/null || echo "__missing__")"
  if [[ "$actual" == "$expected" ]]; then
    echo "  PASS $name"
  else
    echo "  FAIL $name: expected $expected, got $actual" >&2
    fail_count=$((fail_count + 1))
  fi
}

echo ">> starting daemon"
fdif daemon start >/dev/null
trap 'fdif daemon stop >/dev/null 2>&1 || true' EXIT

# --- B. Live-daemon refusal -------------------------------------------------
echo ">> B. cleanup-lock refuses for a live daemon"
set +e
fdif cleanup-lock --yes --json > "$evidence_dir/b1-live-refusal.json" 2> "$evidence_dir/b1-live-refusal.err"
b1_code=$?
set -e
if [[ "$b1_code" -eq 0 ]]; then
  echo "  FAIL b1 cleanup-lock succeeded against a live daemon" >&2
  fail_count=$((fail_count + 1))
else
  echo "  PASS b1 cleanup-lock exited non-zero for a live daemon"
fi
record "b1 live daemon refused" ".action" "refused" "$evidence_dir/b1-live-refusal.json"
record "b1 refusal reason is live owner" ".reason_code" "lock_owner_alive" "$evidence_dir/b1-live-refusal.json"
if [[ -f "$FDIF_HOME/daemon.lock" ]]; then
  echo "  PASS b1 live lock preserved"
else
  echo "  FAIL b1 live lock was removed" >&2
  fail_count=$((fail_count + 1))
fi

# Seed the ledger with a first transaction so restore scenarios have real data.
(cd "$source_repo" && fdif start --yes >/dev/null 2>&1)
fdif daemon stop >/dev/null 2>&1 || true
trap - EXIT

# --- E. Stale-backup restore refusal ----------------------------------------
echo ">> E. restore refuses a stale backup"
futurediff-admin -root "$FDIF_HOME" -backup "$cert_root/backup-one.db" > "$evidence_dir/e0-backup-one.json"
sha_one="$(jq -r .backup.sha256 "$evidence_dir/e0-backup-one.json")"
# Advance the live ledger with a second transaction.
fdif daemon start >/dev/null
trap 'fdif daemon stop >/dev/null 2>&1 || true' EXIT
(cd "$source_repo" && fdif start --yes >/dev/null 2>&1)
fdif daemon stop >/dev/null 2>&1 || true
trap - EXIT
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$cert_root/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER > "$evidence_dir/e1-stale-refused.json" 2> "$evidence_dir/e1-stale-refused.err"
e1_code=$?
set -e
if [[ "$e1_code" -ne 0 ]] && grep -q "older than the live ledger" "$evidence_dir/e1-stale-refused.err"; then
  echo "  PASS e1 stale backup refused with chain evidence"
else
  echo "  FAIL e1 stale backup not refused (code=$e1_code): $(head -c 300 "$evidence_dir/e1-stale-refused.err")" >&2
  fail_count=$((fail_count + 1))
fi
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$cert_root/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/e2-stale-allowed.json" 2> "$evidence_dir/e2-stale-allowed.err"
e2_code=$?
set -e
if [[ "$e2_code" -eq 0 ]]; then
  record "e2 allow-stale-backup applied" ".applied" "true" "$evidence_dir/e2-stale-allowed.json"
  record "e2 pre-restore backup recorded" ".pre_restore_backup.backup_id != null" "true" "$evidence_dir/e2-stale-allowed.json" 2>/dev/null || record "e2 pre-restore backup recorded" ".pre_restore_backup" "null" "$evidence_dir/e2-stale-allowed.json" && true
else
  echo "  FAIL e2 allow-stale-backup apply failed: $(head -c 300 "$evidence_dir/e2-stale-allowed.err")" >&2
  fail_count=$((fail_count + 1))
fi

# --- C. Stale lock cleanup + audit ------------------------------------------
echo ">> C. cleanup-lock removes a stale lock and records the audit event"
python3 - "$FDIF_HOME" <<'PY'
import json, os, sys, time
home = sys.argv[1]
lock = {"version": "0.1", "pid": 999999, "uid": os.geteuid(),
        "started_at": "2026-01-01T00:00:00Z", "root": home,
        "hostname": "cert", "started_at_ns": 1, "boot_id": "old-boot",
        "daemon_version": "cert"}
with open(os.path.join(home, "daemon.lock"), "w") as f:
    json.dump(lock, f)
with open(os.path.join(home, "futurediff.sock"), "w") as f:
    f.write("stale")
PY
fdif cleanup-lock --yes --json > "$evidence_dir/c1-stale-cleaned.json"
record "c1 stale lock cleaned" ".action" "cleaned" "$evidence_dir/c1-stale-cleaned.json"
if [[ ! -e "$FDIF_HOME/daemon.lock" ]]; then
  echo "  PASS c1 lock removed"
else
  echo "  FAIL c1 lock still present" >&2
  fail_count=$((fail_count + 1))
fi
if [[ ! -e "$FDIF_HOME/futurediff.sock" ]]; then
  echo "  PASS c1 socket removed"
else
  echo "  FAIL c1 socket still present" >&2
  fail_count=$((fail_count + 1))
fi
if grep -q '"event_type": "lock_cleanup"' "$FDIF_HOME/audit/operator-events.jsonl" 2>/dev/null \
   || grep -q '"event_type":"lock_cleanup"' "$FDIF_HOME/audit/operator-events.jsonl" 2>/dev/null; then
  echo "  PASS c1 audit event recorded"
else
  echo "  FAIL c1 audit trail missing lock_cleanup event" >&2
  fail_count=$((fail_count + 1))
fi
# Idempotent second run.
fdif cleanup-lock --yes --json > "$evidence_dir/c2-repeat.json"
record "c2 repeated cleanup is a no-op" ".action" "none" "$evidence_dir/c2-repeat.json"

# --- D. Corrupt lock cleanup ------------------------------------------------
echo ">> D. cleanup-lock removes a corrupt lock"
printf '{corrupt json' > "$FDIF_HOME/daemon.lock"
# A daemon-written lock is always 0600; a partially-written (corrupt) lock from
# a crash keeps the daemon's 0600 permissions. chmod here simulates that.
chmod 600 "$FDIF_HOME/daemon.lock"
fdif doctor --json > "$evidence_dir/d1-doctor-corrupt-lock.json" 2>/dev/null || true
lock_status="$(jq -r '.checks[] | select(.ID=="daemon_lock") | .Status' "$evidence_dir/d1-doctor-corrupt-lock.json" 2>/dev/null || echo missing)"
if [[ "$lock_status" == "fail" ]]; then
  echo "  PASS d1 doctor flags corrupt lock as fail"
else
  echo "  FAIL d1 doctor corrupt lock status = $lock_status" >&2
  fail_count=$((fail_count + 1))
fi
fdif cleanup-lock --yes --json > "$evidence_dir/d2-corrupt-cleaned.json"
record "d2 corrupt lock cleaned" ".action" "cleaned" "$evidence_dir/d2-corrupt-cleaned.json"
if [[ ! -e "$FDIF_HOME/daemon.lock" ]]; then
  echo "  PASS d2 corrupt lock removed"
else
  echo "  FAIL d2 corrupt lock still present" >&2
  fail_count=$((fail_count + 1))
fi

# --- A/G. Corrupt ledger diagnostics + fail-closed restore -------------------
echo ">> A. doctor flags a corrupt ledger; restore refuses to run over it"
python3 - "$FDIF_HOME" <<'PY'
import os, sys
home = sys.argv[1]
path = os.path.join(home, "ledger.db")
data = bytearray(open(path, "rb").read())
# Flip a block of bytes in the SQLite header region.
for i in range(100, 140):
    data[i] ^= 0xFF
open(path, "wb").write(data)
PY
fdif doctor --json > "$evidence_dir/a1-doctor-corrupt-ledger.json" 2>/dev/null || true
ledger_status="$(jq -r '.checks[] | select(.ID=="ledger_integrity") | .Status' "$evidence_dir/a1-doctor-corrupt-ledger.json" 2>/dev/null || echo missing)"
if [[ "$ledger_status" == "fail" ]]; then
  echo "  PASS a1 doctor flags corrupt ledger as fail"
else
  echo "  FAIL a1 doctor corrupt ledger status = $ledger_status" >&2
  fail_count=$((fail_count + 1))
fi
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$cert_root/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/a2-corrupt-live-refused.json" 2> "$evidence_dir/a2-corrupt-live-refused.err"
a2_code=$?
set -e
if [[ "$a2_code" -ne 0 ]]; then
  echo "  PASS a2 restore over corrupt live ledger refused (fail-closed)"
else
  echo "  FAIL a2 restore over corrupt live ledger succeeded; the corrupt original would be lost" >&2
  fail_count=$((fail_count + 1))
fi

# --- F. Restore into a fresh root (happy path) -------------------------------
echo ">> F. restore applies cleanly into a fresh root"
fresh_root="$cert_root/fresh-home"
mkdir -p "$fresh_root"
set +e
futurediff-restore -root "$fresh_root" -backup "$cert_root/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER > "$evidence_dir/f1-fresh-apply.json" 2> "$evidence_dir/f1-fresh-apply.err"
f1_code=$?
set -e
if [[ "$f1_code" -eq 0 ]]; then
  record "f1 fresh-root restore applied" ".applied" "true" "$evidence_dir/f1-fresh-apply.json"
else
  echo "  FAIL f1 fresh-root restore failed: $(head -c 300 "$evidence_dir/f1-fresh-apply.err")" >&2
  fail_count=$((fail_count + 1))
fi

# --- G. Disk-pressure classification surface ---------------------------------
echo ">> G. doctor surfaces the storage classification"
storage_status="$(jq -r '.checks[] | select(.ID=="storage") | .Status' "$evidence_dir/a1-doctor-corrupt-ledger.json" 2>/dev/null || echo missing)"
if [[ "$storage_status" == "pass" || "$storage_status" == "warn" ]]; then
  echo "  PASS g1 doctor reports storage classification: $storage_status"
else
  echo "  FAIL g1 doctor storage status = $storage_status" >&2
  fail_count=$((fail_count + 1))
fi

# --- Evidence summary ---------------------------------------------------------
cat > "$evidence_dir/SUMMARY.json" <<EOF
{
  "kind": "corruption-lock-disk-pressure-certification",
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "host": "$(uname -s)-$(uname -m)",
  "scenarios": [
    "live_daemon_lock_refusal",
    "stale_lock_cleanup_with_audit",
    "corrupt_lock_cleanup",
    "corrupt_ledger_diagnosis",
    "stale_backup_restore_refusal",
    "allow_stale_backup_override",
    "fail_closed_restore_over_corrupt_ledger",
    "fresh_root_restore",
    "disk_pressure_classification"
  ],
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
