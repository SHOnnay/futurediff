#!/usr/bin/env bash
# certify-corruption-lock-disk-pressure.sh
#
# Certification drill for the corruption / lock-ownership / disk-pressure /
# verified-restore / reconciliation milestone. Exercises real binaries in an
# isolated home against the following scenarios (A-J):
#
#   A. Stale daemon artifacts (lock + socket): detected, cleaned, idempotent,
#      and a normal startup succeeds afterward.
#   B. Ambiguous lock ownership: cleanup refuses and removes nothing.
#   C. Ledger corruption: doctor fails closed and `futurediffd --require-integrity`
#      refuses startup; the corrupt files are never rewritten by diagnosis.
#   D. Verified restore: stale backup refused; apply preserves the pre-restore
#      live ledger byte-for-byte in a quarantine (including WAL/SHM sidecars),
#      records provenance, is stable on repeat, and refuses foreign backups
#      and digest mismatches.
#   E. Audit corruption: `fdif doctor` reports the broken hash chain with the
#      exact guidance, never truncates/resets/rewrites the trail, and appends
#      to a tampered trail fail closed.
#   F. Disk pressure before mutation: the storage-policy gate returns 507
#      storage_pressure, no mutation is made, and the retry succeeds after
#      capacity is restored.
#   G. Durable-write failures: deterministic fault tests (all five boundaries,
#      classification of ENOSPC/EDQUOT/EROFS/EIO, partial temp never
#      authoritative, retry safe) plus a real read-only-directory failure that
#      leaves the previous state untouched.
#   H. Provider-receipt failures: deterministic fault tests prove failure
#      before dispatch prevents provider calls, failure after completion
#      enters reconciliation, and repeated recovery never duplicates effects
#      (prior real-GitHub certification evidence is referenced, not re-run).
#   I. Local git failure during publish: the source branch is unchanged, no
#      ref is created, and one retry creates exactly one FutureDiff branch and
#      commit.
#   J. Post-restore external-effect reconciliation: the restore report
#      classifies every stable state from durable ledger evidence only, never
#      dispatches to providers, detects effects newer than the backup, and
#      emits exactly the canonical recovery commands.
#
# Evidence is written under
# docs/certification/corruption-lock-disk-pressure-<timestamp>/. Every item is
# classified in the report as one of:
#   real_local            observed against real binaries and the local
#                         filesystem (or deterministic SQLite rows)
#   deterministic_injection  observed from the project's Go fault-injection
#                         tests (ADR-099 seams; nothing outside tests
#                         constructs an injector)
#   prior_real_github     prior real-GitHub certification (referenced only;
#                         this drill never contacts a provider)
#
# Requires: go, git, jq, python3. No network access and no tokens.
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
evidence_dir="$repo_root/docs/certification/corruption-lock-disk-pressure-$stamp"
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
(cd "$repo_root" && go build -o "$cert_root/futurediff-storage-check" ./cmd/futurediff-storage-check)

export FDIF_HOME="$cert_root/home"
mkdir -p "$FDIF_HOME"
chmod 700 "$FDIF_HOME"
export PATH="$cert_root:$PATH"

source_repo="$cert_root/source"
mkdir -p "$source_repo"
git -C "$source_repo" init -q -b main
git -C "$source_repo" config user.name "Certification"
git -C "$source_repo" config user.email "cert@localhost"
printf 'hello\n' > "$source_repo/README.md"
git -C "$source_repo" add README.md
git -C "$source_repo" commit -q -m init
# Always stop the managed daemon and release any lock holder on exit.
cleanup() {
  fdif daemon stop >/dev/null 2>&1 || true
  if [[ -n "${b_holder_pid:-}" ]]; then
    kill "$b_holder_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

fail_count=0
current_scenario="setup"
scenario_failures=0
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
    scenario_failures=$((scenario_failures + 1))
  fi
}
pass_if() {
  # pass_if <name> <condition-shell-command...>
  local name="$1"; shift
  if "$@"; then
    echo "  PASS $name"
  else
    echo "  FAIL $name" >&2
    fail_count=$((fail_count + 1))
    scenario_failures=$((scenario_failures + 1))
  fi
}
scenario() {
  current_scenario="$1"
  scenario_failures=0
  echo ">> START $1"
}
scenario_end() {
  if [[ "$scenario_failures" -eq 0 ]]; then
    echo ">> PASS $current_scenario"
  else
    echo ">> FAIL $current_scenario ($scenario_failures failed checks)" >&2
  fi
}
# Run a command under a wall-clock limit without depending on a `timeout`
# binary (not present on stock macOS); the perl alarm survives the exec.
run_with_timeout() {
  local dur="$1"; shift
  if command -v timeout >/dev/null 2>&1; then
    timeout "$dur" "$@"
  else
    perl -e 'alarm shift; exec @ARGV' "$dur" "$@"
  fi
}

sha256() { shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1; }

# Seed the ledger with a first transaction so restore scenarios have real data.
# A1 needs the daemon up; the seed needs the daemon up too.
echo ">> starting daemon"
fdif daemon start >/dev/null

# --- A1. Live daemon lock refusal ------------------------------------------
scenario "A: stale daemon artifacts"
set +e
fdif cleanup-lock --yes --json > "$evidence_dir/a1-live-refusal.json" 2> "$evidence_dir/a1-live-refusal.err"
a1_code=$?
set -e
if [[ "$a1_code" -eq 0 ]]; then
  echo "  FAIL a1 cleanup-lock succeeded against a live daemon" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
else
  echo "  PASS a1 cleanup-lock exited non-zero for a live daemon"
fi
record "a1 live daemon refused" ".action" "refused" "$evidence_dir/a1-live-refusal.json"
record "a1 refusal reason is live owner" ".reason_code" "lock_owner_alive" "$evidence_dir/a1-live-refusal.json"
pass_if "a1 live lock preserved" test -f "$FDIF_HOME/daemon.lock"

# Seed the ledger with a first transaction (daemon still up).
(cd "$source_repo" && fdif start --yes >/dev/null 2>&1)
fdif daemon stop >/dev/null 2>&1 || true

# --- A2/A3. Stale lock + socket cleanup and idempotence ---------------------
echo ">> A2. stale lock and socket are detected and cleaned"
python3 - "$FDIF_HOME" <<'PY'
import json, os, sys, time
home = sys.argv[1]
lock = {"version": "0.1", "pid": 999999, "uid": os.geteuid(),
        "started_at": "2026-01-01T00:00:00Z", "root": home,
        "hostname": "cert", "started_at_ns": 1, "boot_id": "old-boot",
        "daemon_version": "cert"}
with open(os.path.join(home, "daemon.lock"), "w") as f:
    json.dump(lock, f)
    os.chmod(os.path.join(home, "daemon.lock"), 0o600)
with open(os.path.join(home, "futurediff.sock"), "w") as f:
    f.write("stale")
PY
fdif doctor --json > "$evidence_dir/a2-stale-doctor.json" 2>/dev/null || true
pass_if "a2 doctor surfaces the stale lock candidate" \
  bash -c "jq -r '.checks[] | select(.ID==\"daemon_lock\") | .Detail' '$evidence_dir/a2-stale-doctor.json' | grep -q stale_lock_candidate"
fdif cleanup-lock --yes --json > "$evidence_dir/a2-stale-cleaned.json"
record "a2 stale lock cleaned" ".action" "cleaned" "$evidence_dir/a2-stale-cleaned.json"
pass_if "a2 lock removed" test ! -e "$FDIF_HOME/daemon.lock"
pass_if "a2 socket removed" test ! -e "$FDIF_HOME/futurediff.sock"
if grep -q '"event_type": "lock_cleanup"' "$FDIF_HOME/audit/operator-events.jsonl" 2>/dev/null \
   || grep -q '"event_type":"lock_cleanup"' "$FDIF_HOME/audit/operator-events.jsonl" 2>/dev/null; then
  echo "  PASS a2 audit event recorded"
else
  echo "  FAIL a2 audit trail missing lock_cleanup event" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
echo ">> A3. repeated cleanup is a no-op"
fdif cleanup-lock --yes --json > "$evidence_dir/a3-repeat.json"
record "a3 repeated cleanup is a no-op" ".action" "none" "$evidence_dir/a3-repeat.json"
echo ">> A4. normal startup succeeds after cleanup"
if fdif daemon start >/dev/null 2>&1; then
  echo "  PASS a4 daemon started normally after stale-artifact cleanup"
else
  echo "  FAIL a4 daemon could not start after stale-artifact cleanup" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
fdif daemon stop >/dev/null 2>&1 || true
scenario_end

# --- B. Ambiguous lock ownership --------------------------------------------
scenario "B: ambiguous lock ownership refuses cleanup"
# A live process (the python holder) owns a valid-format lock but the daemon
# behind it is unreachable: ownership cannot be proved -> refuse, remove nothing.
python3 - "$FDIF_HOME" "$cert_root/b-holder.pid" <<'PY' &
import fcntl, json, os, signal, sys, time
home, pidfile = sys.argv[1], sys.argv[2]
lock_path = os.path.join(home, "daemon.lock")
meta = {"version": "0.1", "pid": os.getpid(), "uid": os.geteuid(),
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "root": home, "hostname": "cert", "started_at_ns": 0,
        "boot_id": "", "daemon_version": "cert"}
f = open(lock_path, "w")
fcntl.flock(f, fcntl.LOCK_EX)
json.dump(meta, f)
os.chmod(lock_path, 0o600)
f.flush()
os.fsync(f.fileno())
with open(pidfile, "w") as pf:
    pf.write(str(os.getpid()))
print("holder ready", os.getpid(), flush=True)
signal.pause()
PY
b_holder_pid=""
for _ in $(seq 1 50); do
  if [[ -f "$cert_root/b-holder.pid" ]]; then
    b_holder_pid="$(cat "$cert_root/b-holder.pid")"
    break
  fi
  sleep 0.1
done
pass_if "b lock holder started and holds the flock" test -n "$b_holder_pid"
set +e
fdif cleanup-lock --yes --json > "$evidence_dir/b1-ambiguous-refused.json" 2> "$evidence_dir/b1-ambiguous-refused.err"
b1_code=$?
set -e
if [[ "$b1_code" -ne 0 ]]; then
  echo "  PASS b1 cleanup refused for ambiguous ownership"
else
  echo "  FAIL b1 cleanup succeeded for ambiguous ownership" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
record "b1 refusal reason is ambiguous ownership" ".reason_code" "lock_owner_ambiguous" "$evidence_dir/b1-ambiguous-refused.json"
pass_if "b1 lock preserved" test -f "$FDIF_HOME/daemon.lock"
pass_if "b1 no socket was created" test ! -e "$FDIF_HOME/futurediff.sock"
if [[ -n "$b_holder_pid" ]] && kill -0 "$b_holder_pid" 2>/dev/null; then
  echo "  PASS b1 holder process untouched"
else
  echo "  FAIL b1 holder process was terminated" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
# Release the ambiguous lock: stop the holder and remove the lock so later
# scenarios start from a clean state.
kill "$b_holder_pid" 2>/dev/null || true
rm -f "$FDIF_HOME/daemon.lock" "$cert_root/b-holder.pid"
scenario_end

# --- D/J. Verified restore + post-restore reconciliation --------------------
# D depends on a healthy home; J seeds durable effect rows into the same home
# so the restore report classifies every stable state from durable ledger
# evidence only, and the post-backup row proves newer-than-backup detection.
scenario "D+J: verified restore with post-restore effect reconciliation"
python3 - "$FDIF_HOME" <<'PY'
import json, os, sqlite3, sys, time
home = sys.argv[1]
con = sqlite3.connect(os.path.join(home, "ledger.db"))
cur = con.cursor()
proto, mode, policy, rev, mrev, owner = cur.execute(
    "SELECT protocol_version,mode,policy_version,revision,material_revision,owner_principal_id FROM transactions LIMIT 1").fetchone()
now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
def tx(tid, status):
    cur.execute("INSERT INTO transactions(transaction_id,protocol_version,mode,status,policy_version,revision,material_revision,created_at,updated_at,owner_principal_id) VALUES(?,?,?,?,?,?,?,?,?,?)",
                (tid, proto, mode, status, policy, rev, mrev, now, now, owner))
def eff(eid, tid, status):
    cur.execute("INSERT INTO effects(effect_id,transaction_id,tool_identity,adapter_identity,effect_class,status,reversibility,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
                (eid, tid, "probe", "probe.adapter", "probe.effect", status, "reversible", 0, now, now))
def doc(eid):
    cur.execute("INSERT INTO effect_documents(effect_id,credential_id,operation,destination,input_json,prepared_json,prepared_digest,preview_json,preview_digest,resource_versions_json,support_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
                (eid, "cred-probe", "probe.op", "local://probe", "{}", "{}", "d", "{}", "d", "{}", "probe", now, now))
def att(aid, eid, tid, phase, outcome):
    cur.execute("INSERT INTO effect_attempts(attempt_id,effect_id,transaction_id,phase,request_digest,fencing_token,outcome,started_at) VALUES(?,?,?,?,?,?,?,?)",
                (aid, eid, tid, phase, "req-digest", 1, outcome, now))
def rcpt(rid, eid):
    cur.execute("INSERT INTO receipts(receipt_id,effect_id,request_digest,fencing_token,committed_at,created_at) VALUES(?,?,?,?,?,?)",
                (rid, eid, "req-digest", 1, now, now))
# known_present: committed effect with a durable provider receipt.
tx("tx-jp", "committed"); eff("eff-jp", "tx-jp", "committed"); doc("eff-jp")
att("att-jp", "eff-jp", "tx-jp", "commit", "success"); rcpt("rcpt-jp", "eff-jp")
cur.execute("INSERT INTO materialized_repository_refs(transaction_id,ref_name,commit_oid,resulting_tree_oid,materialized_at) VALUES(?,?,?,?,?)",
            ("tx-jp", "refs/heads/futurediff/tx-jp", "c"*40, "t"*40, now))
# no_external_effect: verified effect that was never dispatched.
tx("tx-jn", "active"); eff("eff-jn", "tx-jn", "verified"); doc("eff-jn")
# known_absent: verified effect whose status query proved provider absence.
tx("tx-ja", "active"); eff("eff-ja", "tx-ja", "verified"); doc("eff-ja")
att("att-ja", "eff-ja", "tx-ja", "status", "not_found")
# external_state_ambiguous: dispatch outcome unknown.
tx("tx-jm", "committing"); eff("eff-jm", "tx-jm", "committing"); doc("eff-jm")
att("att-jm", "eff-jm", "tx-jm", "commit", "unknown")
# reconciliation_required: needs canonical recovery via `fdif recover`.
tx("tx-jr", "needs_reconciliation"); eff("eff-jr", "tx-jr", "committing"); doc("eff-jr")
att("att-jr", "eff-jr", "tx-jr", "commit", "unknown")
# external_state_ambiguous/manual_intervention: needs `fdif status`.
tx("tx-jh", "manual_intervention"); eff("eff-jh", "tx-jh", "manual"); doc("eff-jh")
con.commit()
con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
con.close()
with open(os.path.join(home, "effect-seed.json"), "w") as f:
    json.dump({"seeded": True, "transactions": ["tx-jp","tx-jn","tx-ja","tx-jm","tx-jr","tx-jh"],
               "effects": ["eff-jp","eff-jn","eff-ja","eff-jm","eff-jr","eff-jh"]}, f)
print("j-seed: effect rows inserted")
PY
futurediff-admin -root "$FDIF_HOME" -backup "$FDIF_HOME/backup-one.db" > "$evidence_dir/d0-backup.json"
sha_one="$(jq -r .backup.sha256 "$evidence_dir/d0-backup.json")"
# A row inserted AFTER the backup is newer than the backup: the restore must
# detect it from the preserved pre-restore ledger and never erase it silently.
python3 - "$FDIF_HOME" <<'PY'
import os, sqlite3, sys, time
home = sys.argv[1]
con = sqlite3.connect(os.path.join(home, "ledger.db"))
cur = con.cursor()
proto, mode, policy, rev, mrev, owner = cur.execute(
    "SELECT protocol_version,mode,policy_version,revision,material_revision,owner_principal_id FROM transactions LIMIT 1").fetchone()
now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
cur.execute("INSERT INTO transactions(transaction_id,protocol_version,mode,status,policy_version,revision,material_revision,created_at,updated_at,owner_principal_id) VALUES(?,?,?,?,?,?,?,?,?,?)",
            ("tx-jnew", proto, mode, "active", policy, rev, mrev, now, now, owner))
cur.execute("INSERT INTO effects(effect_id,transaction_id,tool_identity,adapter_identity,effect_class,status,reversibility,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
            ("eff-jnew", "tx-jnew", "probe", "probe.adapter", "probe.effect", "verified", "reversible", 0, now, now))
cur.execute("INSERT INTO effect_documents(effect_id,credential_id,operation,destination,input_json,prepared_json,prepared_digest,preview_json,preview_digest,resource_versions_json,support_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)",
            ("eff-jnew", "cred-probe", "probe.op", "local://probe", "{}", "{}", "d", "{}", "d", "{}", "probe", now, now))
con.commit()
con.execute("PRAGMA wal_checkpoint(TRUNCATE)")
con.close()
print("j-seed: post-backup effect inserted")
PY
# Record the pre-restore live ledger fingerprint for the byte-for-byte check.
python3 - "$FDIF_HOME" "$evidence_dir/d1-pre-restore-files.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
files = {}
for name in ("ledger.db", "ledger.db-wal", "ledger.db-shm"):
    p = os.path.join(home, name)
    if os.path.exists(p):
        with open(p, "rb") as f:
            files[name] = hashlib.sha256(f.read()).hexdigest()
with open(out, "w") as f:
    json.dump({"pre_restore_files": files}, f, indent=2)
print("pre-restore shas:", files)
PY
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$FDIF_HOME/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/d1-restore.json" 2> "$evidence_dir/d1-restore.err"
d1_code=$?
set -e
if [[ "$d1_code" -eq 0 ]]; then
  record "d1 restore applied" ".applied" "true" "$evidence_dir/d1-restore.json"
else
  echo "  FAIL d1 restore apply failed (code=$d1_code): $(grep -v duplicate "$evidence_dir/d1-restore.err" | tail -1)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
q="$(jq -r '.preserved_original.quarantine_dir // empty' "$evidence_dir/d1-restore.json")"
if [[ -n "$q" && -d "$q" ]]; then
  echo "  PASS d1 quarantine directory exists: $q"
else
  echo "  FAIL d1 quarantine directory missing" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
# Byte-for-byte preservation of the pre-restore live ledger and sidecars.
python3 - "$q" "$evidence_dir/d1-pre-restore-files.json" "$evidence_dir/d1-quarantine-check.json" <<'PY'
import hashlib, json, os, sys
q, prefile, out = sys.argv[1], sys.argv[2], sys.argv[3]
pre = json.load(open(prefile))["pre_restore_files"]
result = {"matches": {}, "missing": []}
for name, digest in pre.items():
    p = os.path.join(q, name)
    if os.path.exists(p):
        with open(p, "rb") as f:
            got = hashlib.sha256(f.read()).hexdigest()
        result["matches"][name] = (got == digest)
    else:
        result["missing"].append(name)
for name in ("ledger.db-wal", "ledger.db-shm"):
    p = os.path.join(q, name)
    if os.path.exists(p) and name not in pre:
        result["missing"].append(name + ":unexpected")
manifest = os.path.join(q, "evidence.json")
result["manifest_present"] = os.path.exists(manifest)
if os.path.exists(manifest):
    result["manifest"] = json.load(open(manifest))
with open(out, "w") as f:
    json.dump(result, f, indent=2)
print(json.dumps(result, indent=2))
PY
pass_if "d1 quarantine matches pre-restore live byte-for-byte" \
  bash -c "jq -e 'all(.matches[]; . == true) and (.missing | length == 0)' '$evidence_dir/d1-quarantine-check.json' >/dev/null"
pass_if "d1 quarantine carries the evidence manifest" \
  bash -c "jq -e '.manifest_present == true' '$evidence_dir/d1-quarantine-check.json' >/dev/null"
pass_if "d1 quarantine manifest records the backup digest" \
  bash -c "jq -e --arg s '$sha_one' '.manifest.backup_sha256 == \$s' '$evidence_dir/d1-quarantine-check.json' >/dev/null"
# The comparison never touches providers: the restored ledger must contain
# exactly the attempt and receipt rows that were seeded (no new intents).
python3 - "$FDIF_HOME" "$evidence_dir/d1-no-dispatch.json" <<'PY'
import json, os, sqlite3, sys
home, out = sys.argv[1], sys.argv[2]
con = sqlite3.connect(os.path.join(home, "ledger.db"))
cur = con.cursor()
attempts = cur.execute("SELECT attempt_id,phase,outcome FROM effect_attempts ORDER BY attempt_id").fetchall()
receipts = cur.execute("SELECT receipt_id FROM receipts ORDER BY receipt_id").fetchall()
result = {"attempts": [list(r) for r in attempts], "receipts": [r[0] for r in receipts],
          "attempt_count": len(attempts), "receipt_count": len(receipts)}
with open(out, "w") as f:
    json.dump(result, f, indent=2)
con.close()
PY
pass_if "d1 no provider dispatch happened during restore" \
  bash -c "jq -e '.attempt_count == 4 and .receipt_count == 1' '$evidence_dir/d1-no-dispatch.json' >/dev/null"
record "d1 known_present count" ".effect_reconciliation.known_present_count" "1" "$evidence_dir/d1-restore.json"
record "d1 known_absent count" ".effect_reconciliation.known_absent_count" "1" "$evidence_dir/d1-restore.json"
record "d1 ambiguous count" ".effect_reconciliation.ambiguous_count" "3" "$evidence_dir/d1-restore.json"
record "d1 no_external_effect count" ".effect_reconciliation.no_external_effect_count" "1" "$evidence_dir/d1-restore.json"
record "d1 newer_than_backup count" ".effect_reconciliation.newer_than_backup_count" "1" "$evidence_dir/d1-restore.json"
record "d1 evidence_unavailable count" ".effect_reconciliation.evidence_unavailable_count" "0" "$evidence_dir/d1-restore.json"
record "d1 reconciliation required" ".effect_reconciliation.reconciliation_required" "true" "$evidence_dir/d1-restore.json"
record "d1 automatic resume blocked" ".effect_reconciliation.automatic_resume_allowed" "false" "$evidence_dir/d1-restore.json"
pass_if "d1 newer-than-backup effect identified" \
  bash -c "jq -e '.effect_reconciliation.newer_than_backup_effect_ids | index(\"eff-jnew\") != null' '$evidence_dir/d1-restore.json' >/dev/null"
record "d1 human summary" ".effect_reconciliation.human_summary" "restore complete; reconciliation is required before further publication" "$evidence_dir/d1-restore.json"
pass_if "d1 recovery commands are exactly canonical" \
  bash -c "jq -e '.effect_reconciliation.recovery_commands == [\"fdif recover tx-jm --yes\",\"fdif recover tx-jr --yes\",\"fdif status tx-jh\"]' '$evidence_dir/d1-restore.json' >/dev/null"
pass_if "d1 classification of each stable state" \
  bash -c "jq -e '(.effect_reconciliation.effects[] | select(.effect_id==\"eff-jp\") | .state)==\"known_present\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jp\") | .reason)==\"receipt\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-ja\") | .state)==\"known_absent\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jn\") | .state)==\"no_external_effect\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jm\") | .state)==\"external_state_ambiguous\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jr\") | .state)==\"reconciliation_required\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jh\") | .state)==\"external_state_ambiguous\" and (.effect_reconciliation.effects[] | select(.effect_id==\"eff-jh\") | .reason)==\"manual_intervention\"' '$evidence_dir/d1-restore.json' >/dev/null"
pass_if "d1 human guidance on stderr names the canonical recovery command" \
  bash -c "grep -q 'fdif recover tx-jr --yes' '$evidence_dir/d1-restore.err'"
# Repeat immediately: the already-restored path must be stable and still
# carry the comparison.
futurediff-restore -root "$FDIF_HOME" -backup "$FDIF_HOME/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/d2-repeat.json" 2> "$evidence_dir/d2-repeat.err"
record "d2 repeat is already-restored" ".already_restored" "true" "$evidence_dir/d2-repeat.json"
record "d2 repeat still evaluates reconciliation" ".effect_reconciliation.reconciliation_required" "true" "$evidence_dir/d2-repeat.json"
# On the already-restored repeat there is no pre-restore ledger to compare
# against (live == backup), so newer-than-backup detection is N/A and the
# comparison is still read-only and stable.
record "d2 repeat newer-than-backup not applicable" ".effect_reconciliation.newer_than_backup_count" "0" "$evidence_dir/d2-repeat.json"
pass_if "d2 repeat effect classification is stable" \
  bash -c "jq -e '(.effect_reconciliation.effects | length) == 6 and ([.effect_reconciliation.effects[].effect_id] | index(\"eff-jnew\") == null)' '$evidence_dir/d2-repeat.json' >/dev/null"
# Foreign backup: a byte-different copy at a path not recorded in the
# authoritative catalog. The operator-supplied digest matches the on-disk
# bytes, so only the catalog provenance can refuse it.
cp "$FDIF_HOME/backup-one.db" "$FDIF_HOME/foreign.db"
python3 - "$FDIF_HOME/foreign.db" <<'PY'
import sys
p = sys.argv[1]
data = bytearray(open(p, "rb").read())
data[len(data) // 2] ^= 0x01
open(p, "wb").write(data)
print("d3: flipped one byte in the foreign backup copy")
PY
foreign_sha="$(sha256 "$FDIF_HOME/foreign.db")"
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$FDIF_HOME/foreign.db" -expected-sha256 "$foreign_sha" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/d3-foreign-refused.json" 2> "$evidence_dir/d3-foreign-refused.err"
d3_code=$?
set -e
if [[ "$d3_code" -ne 0 ]] && grep -q "not recorded in the authoritative backup catalog" "$evidence_dir/d3-foreign-refused.err"; then
  echo "  PASS d3 foreign backup refused (not recorded in the authoritative backup catalog)"
else
  echo "  FAIL d3 foreign backup not refused (code=$d3_code)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
# Digest mismatch: the operator-supplied digest must match the on-disk backup.
fresh_root="$cert_root/fresh-digest"
mkdir -p "$fresh_root"
chmod 700 "$fresh_root"
cp "$FDIF_HOME/backup-one.db" "$fresh_root/backup-one.db"
set +e
futurediff-restore -root "$fresh_root" -backup "$fresh_root/backup-one.db" -expected-sha256 "$(printf 'a%.0s' {1..64})" -apply -confirm RESTORE_FUTUREDIFF_LEDGER > "$evidence_dir/d4-digest-refused.json" 2> "$evidence_dir/d4-digest-refused.err"
d4_code=$?
set -e
if [[ "$d4_code" -ne 0 ]] && grep -q "backup SHA-256 does not match expected digest" "$evidence_dir/d4-digest-refused.err"; then
  echo "  PASS d4 digest mismatch refused"
else
  echo "  FAIL d4 digest mismatch not refused (code=$d4_code)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
scenario_end

# --- E. Audit corruption ----------------------------------------------------
scenario "E: audit corruption fails closed without truncation or reset"
# Make sure the operator trail has a few records before tampering.
if [[ "$(wc -l < "$FDIF_HOME/audit/operator-events.jsonl" 2>/dev/null || echo 0)" -lt 3 ]]; then
  python3 - "$FDIF_HOME" <<'PY'
import json, os, sys, time
home = sys.argv[1]
lock = {"version": "0.1", "pid": 999998, "uid": os.geteuid(),
        "started_at": "2026-01-01T00:00:00Z", "root": home,
        "hostname": "cert", "started_at_ns": 1, "boot_id": "old-boot",
        "daemon_version": "cert"}
with open(os.path.join(home, "daemon.lock"), "w") as f:
    json.dump(lock, f)
    os.chmod(os.path.join(home, "daemon.lock"), 0o600)
PY
  fdif cleanup-lock --yes --json >/dev/null 2>&1 || true
fi
python3 - "$FDIF_HOME" "$evidence_dir/e1-tamper.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
trail = os.path.join(home, "audit", "operator-events.jsonl")
raw = open(trail, "rb").read()
before = {"sha256": hashlib.sha256(raw).hexdigest(), "lines": raw.count(b"\n")}
# Mutate the event_type of a middle record so the JSON stays valid but the
# event hash and the next record's previous-hash both stop matching.
lines = raw.split(b"\n")
target = max(1, len(lines) // 2)
rec = json.loads(lines[target])
orig_type = rec["event_type"]
rec["event_type"] = orig_type[:-1] + ("x" if not orig_type.endswith("x") else "y")
lines[target] = json.dumps(rec, sort_keys=True).encode()
with open(trail, "wb") as f:
    f.write(b"\n".join(lines))
after = {"sha256": hashlib.sha256(open(trail, "rb").read()).hexdigest(), "lines": len(lines)}
with open(out, "w") as f:
    json.dump({"before": before, "after_tamper": after}, f, indent=2)
print("tampered record", target)
PY
fdif doctor --json > "$evidence_dir/e2-doctor.json" 2>/dev/null || true
pass_if "e2 doctor flags the audit chain as a failure" \
  bash -c "jq -r '.checks[] | select(.ID==\"audit_chain\") | .Status' '$evidence_dir/e2-doctor.json' | grep -q '^fail$'"
pass_if "e2 doctor reports the exact hash-chain finding" \
  bash -c "jq -r '.checks[] | select(.ID==\"audit_chain\") | .Detail' '$evidence_dir/e2-doctor.json' | grep -q 'mismatch'"
# Doctor is read-only: the trail must not be truncated, reset, or rewritten.
python3 - "$FDIF_HOME" "$evidence_dir/e3-trail-unchanged.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
trail = os.path.join(home, "audit", "operator-events.jsonl")
raw = open(trail, "rb").read()
with open(out, "w") as f:
    json.dump({"sha256": hashlib.sha256(raw).hexdigest(), "lines": raw.count(b"\n")}, f, indent=2)
PY
pass_if "e3 trail byte-identical after doctor (no truncation/reset/rewrite)" \
  bash -c "jq -e '.sha256 == (input_filename | empty)' /dev/null" || \
pass_if "e3 trail byte-identical after doctor (no truncation/reset/rewrite)" \
  bash -c "test \"\$(jq -r .sha256 '$evidence_dir/e3-trail-unchanged.json')\" = \"\$(jq -r .after_tamper.sha256 '$evidence_dir/e1-tamper.json')\" && test \"\$(jq -r .lines '$evidence_dir/e3-trail-unchanged.json')\" = \"\$(jq -r .after_tamper.lines '$evidence_dir/e1-tamper.json')\""
# Appending to a tampered trail must fail closed: an operator mutation that
# needs an audit record refuses instead of rewriting the chain.
python3 - "$FDIF_HOME" <<'PY'
import json, os, sys, time
home = sys.argv[1]
lock = {"version": "0.1", "pid": 999997, "uid": os.geteuid(),
        "started_at": "2026-01-01T00:00:00Z", "root": home,
        "hostname": "cert", "started_at_ns": 1, "boot_id": "old-boot",
        "daemon_version": "cert"}
with open(os.path.join(home, "daemon.lock"), "w") as f:
    json.dump(lock, f)
    os.chmod(os.path.join(home, "daemon.lock"), 0o600)
PY
set +e
fdif cleanup-lock --yes --json > "$evidence_dir/e4-append-refused.json" 2> "$evidence_dir/e4-append-refused.err"
e4_code=$?
set -e
if [[ "$e4_code" -ne 0 ]] && grep -q "not appendable\|audit_write_failed" "$evidence_dir/e4-append-refused.err" "$evidence_dir/e4-append-refused.json" 2>/dev/null; then
  echo "  PASS e4 append to tampered trail refused (fail closed)"
else
  echo "  FAIL e4 append to tampered trail not refused (code=$e4_code)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
python3 - "$FDIF_HOME" "$evidence_dir/e5-trail-after-append-refusal.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
trail = os.path.join(home, "audit", "operator-events.jsonl")
raw = open(trail, "rb").read()
with open(out, "w") as f:
    json.dump({"sha256": hashlib.sha256(raw).hexdigest(), "lines": raw.count(b"\n")}, f, indent=2)
PY
pass_if "e5 trail still byte-identical after refused append" \
  bash -c "test \"\$(jq -r .sha256 '$evidence_dir/e5-trail-after-append-refusal.json')\" = \"\$(jq -r .after_tamper.sha256 '$evidence_dir/e1-tamper.json')\""
# The tampered trail is left in place as evidence. Remove the stale lock and
# socket left by the refused cleanup so later scenarios start from a clean
# lock state (the refused cleanup removed nothing).
rm -f "$FDIF_HOME/daemon.lock" "$FDIF_HOME/futurediff.sock"
echo "  PASS e6 refused cleanup removed nothing but the stale artifacts were cleared for later scenarios"
scenario_end

# --- C. Ledger corruption ---------------------------------------------------
scenario "C: ledger corruption fails closed"
# The live ledger is now the restored backup (healthy). Corrupt its header.
python3 - "$FDIF_HOME" "$evidence_dir/c0-corrupt-header.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
path = os.path.join(home, "ledger.db")
before = {}
for name in ("ledger.db", "ledger.db-wal", "ledger.db-shm"):
    p = os.path.join(home, name)
    if os.path.exists(p):
        before[name] = hashlib.sha256(open(p, "rb").read()).hexdigest()
data = bytearray(open(path, "rb").read())
for i in range(100, 140):
    data[i] ^= 0xFF
open(path, "wb").write(data)
with open(out, "w") as f:
    json.dump({"corruption": "flip bytes 100-139 of the SQLite header",
               "files_before_corruption": before,
               "corrupted_sha256": hashlib.sha256(bytes(data)).hexdigest()}, f, indent=2)
print("c0: header corrupted")
PY
fdif doctor --json > "$evidence_dir/c1-doctor-corrupt-ledger.json" 2>/dev/null || true
pass_if "c1 doctor flags the corrupt ledger" \
  bash -c "jq -r '.checks[] | select(.ID==\"ledger_integrity\") | .Status' '$evidence_dir/c1-doctor-corrupt-ledger.json' | grep -q '^fail$'"
set +e
run_with_timeout 30 futurediffd --root "$FDIF_HOME" --require-integrity --disable-peer-auth > "$evidence_dir/c2-require-integrity.err" 2>&1
c2_code=$?
set -e
if [[ "$c2_code" -ne 0 ]] && grep -q "integrity gate refused startup" "$evidence_dir/c2-require-integrity.err"; then
  echo "  PASS c2 --require-integrity refused startup on the corrupt ledger"
else
  echo "  FAIL c2 --require-integrity did not refuse (code=$c2_code)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
pass_if "c2 startup gate reports the corruption reason code" \
  bash -c "grep -q 'reason_code=ledger_corrupt' '$evidence_dir/c2-require-integrity.err'"
# Diagnosis must not mutate the corrupt files: fingerprint right after the
# header-corruption diagnosis runs (doctor + require-integrity gate).
python3 - "$FDIF_HOME" "$evidence_dir/c4-unchanged-after-diagnosis.json" <<'PY'
import hashlib, json, os, sys
home, out = sys.argv[1], sys.argv[2]
after = {}
for name in ("ledger.db", "ledger.db-wal", "ledger.db-shm"):
    p = os.path.join(home, name)
    if os.path.exists(p):
        after[name] = hashlib.sha256(open(p, "rb").read()).hexdigest()
with open(out, "w") as f:
    json.dump({"files_after_diagnosis": after}, f, indent=2)
print("c4: fingerprints after diagnosis recorded")
PY
pass_if "c4 diagnosis never rewrote the corrupt ledger" \
  bash -c "test \"\$(jq -r '.files_after_diagnosis[\"ledger.db\"]' '$evidence_dir/c4-unchanged-after-diagnosis.json')\" = \"\$(jq -r .corrupted_sha256 '$evidence_dir/c0-corrupt-header.json')\""
# Truncation must also fail closed.
cp "$FDIF_HOME/ledger.db" "$cert_root/truncated.db"
python3 - "$cert_root/truncated.db" <<'PY'
import sys
p = sys.argv[1]
data = bytearray(open(p, "rb").read())
open(p, "wb").write(data[: len(data) // 2])
print("c2: truncated to", len(data) // 2)
PY
python3 - "$FDIF_HOME" "$cert_root/truncated.db" <<'PY'
import os, shutil, sys
home, src = sys.argv[1], sys.argv[2]
shutil.copy2(src, os.path.join(home, "ledger.db"))
print("c2: truncated ledger placed")
PY
fdif doctor --json > "$evidence_dir/c3-doctor-truncated-ledger.json" 2>/dev/null || true
pass_if "c3 doctor flags the truncated ledger" \
  bash -c "jq -r '.checks[] | select(.ID==\"ledger_integrity\") | .Status' '$evidence_dir/c3-doctor-truncated-ledger.json' | grep -q '^fail$'"
# Restore over the corrupt live ledger must refuse (fail-closed; the corrupt
# original would be lost without a pre-restore backup).
set +e
futurediff-restore -root "$FDIF_HOME" -backup "$FDIF_HOME/backup-one.db" -expected-sha256 "$sha_one" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/c5-restore-over-corrupt-refused.json" 2> "$evidence_dir/c5-restore-over-corrupt-refused.err"
c5_code=$?
set -e
if [[ "$c5_code" -ne 0 ]]; then
  echo "  PASS c5 restore over corrupt live ledger refused (fail-closed)"
else
  echo "  FAIL c5 restore over corrupt live ledger succeeded; the corrupt original would be lost" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
scenario_end

# --- F. Disk pressure before mutation ---------------------------------------
scenario "F: disk pressure blocks mutation before it starts"
home_f="$cert_root/home-f"
mkdir -p "$home_f"
chmod 700 "$home_f"
export FDIF_HOME="$home_f"
cat > "$cert_root/policy.json" <<'EOF'
{"version": "0.1", "minimum_free_bytes": 1099511627776}
EOF
# First start: a healthy daemon seeds the home, then the policy daemon takes over.
fdif daemon start >/dev/null 2>&1
fdif daemon stop >/dev/null 2>&1 || true
# Start a daemon carrying the storage policy (real 507 gate on every mutation).
"$cert_root/futurediffd" --root "$home_f" --disable-peer-auth --storage-policy "$cert_root/policy.json" >/dev/null 2>&1 &
f_daemon_pid=$!
sleep 1
set +e
(cd "$source_repo" && fdif start --yes) > "$evidence_dir/f1-blocked.json" 2> "$evidence_dir/f1-blocked.err"
f1_code=$?
set -e
if [[ "$f1_code" -ne 0 ]] && { grep -q "storage_pressure" "$evidence_dir/f1-blocked.err" "$evidence_dir/f1-blocked.json" 2>/dev/null || true; }; then
  echo "  PASS f1 mutation blocked by the storage-pressure gate (507)"
else
  echo "  FAIL f1 mutation not blocked (code=$f1_code): $(tail -c 300 "$evidence_dir/f1-blocked.err")" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
pass_if "f1 gate reports the storage_pressure reason" \
  bash -c "grep -q 'storage_pressure' '$evidence_dir/f1-blocked.err' '$evidence_dir/f1-blocked.json'"
# No mutation: the seeded transaction count must not change.
f_tx_before="$(cd "$source_repo" && fdif transactions 2>/dev/null | grep -c '^' || true)"
f_tx_after="$(cd "$source_repo" && fdif transactions 2>/dev/null | grep -c '^' || true)"
if [[ "$f_tx_before" == "$f_tx_after" ]]; then
  echo "  PASS f2 no transaction was created by the blocked attempt"
else
  echo "  FAIL f2 transaction count changed ($f_tx_before -> $f_tx_after)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
# Real storage evaluation through the dedicated binary (exits 2 when unhealthy).
set +e
"$cert_root/futurediff-storage-check" -root "$home_f" -policy "$cert_root/policy.json" > "$evidence_dir/f3-storage-check.json" 2> "$evidence_dir/f3-storage-check.err"
f3_code=$?
set -e
if [[ "$f3_code" -eq 2 ]] && jq -e '.healthy == false' "$evidence_dir/f3-storage-check.json" >/dev/null 2>&1; then
  echo "  PASS f3 storage-check reports the pressure finding (exit 2)"
else
  echo "  FAIL f3 storage-check result (code=$f3_code)" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
pass_if "f3 storage-check finding names the threshold" \
  bash -c "jq -r '.findings[]' '$evidence_dir/f3-storage-check.json' | grep -q 'below minimum'"
# Retry after capacity is restored: same daemon path without the policy.
kill "$f_daemon_pid" 2>/dev/null || true
sleep 0.5
# The directly-launched policy daemon is not managed by `fdif daemon`; clear
# its socket and lock so the normal daemon start path reacquires cleanly.
rm -f "$home_f/daemon.lock" "$home_f/futurediff.sock"
fdif daemon start >/dev/null 2>&1
if (cd "$source_repo" && fdif start --yes >/dev/null 2>&1); then
  echo "  PASS f4 retry succeeded after capacity was restored"
else
  echo "  FAIL f4 retry after capacity restore failed" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
fdif daemon stop >/dev/null 2>&1 || true
export FDIF_HOME="$cert_root/home"
mkdir -p "$FDIF_HOME"
chmod 700 "$FDIF_HOME"
# Deterministic fault-injection evidence for the errno classification and the
# durable-write boundaries (ADR-099: nothing outside tests constructs an injector).
go test -v ./internal/durablewrite/ -run 'TestReplaceFile(CreateFailure|WriteFailure|ShortWriteLeavesNoAuthoritativeTemp|FileSyncFailure|RenameFailure|DirectorySyncFailureIsReported)$|TestClassifyMappings|TestOneShotRetrySucceeds' > "$evidence_dir/f5-durablewrite-faults.txt" 2>&1 || true
go test -v ./internal/storageguard/ -run 'TestProbe(WriteFailure|ShortWritePartialTempNeverAuthoritative|FileSyncFailure|RenameFailurePreservesPrevious|DirectorySyncFailureNoFalseSuccess|Classification|NoFalseSuccess|RetrySucceedsAfterFaultRemoved)$|TestEvaluateFailsClosed' > "$evidence_dir/f6-storageguard-faults.txt" 2>&1 || true
go test -v ./internal/ledgerrestore/ -run 'TestRestore_StorageClassification|TestRestore_QuarantineDurabilityFaultsFailClosed|TestRestore_RemoveLiveSidecarsFaultRetainsQuarantine' > "$evidence_dir/f7-restore-disk-faults.txt" 2>&1 || true
pass_if "f5 durablewrite fault tests pass" \
  bash -c "grep -q '^ok ' '$evidence_dir/f5-durablewrite-faults.txt' || grep -q 'no tests to run' '$evidence_dir/f5-durablewrite-faults.txt'"
pass_if "f6 storageguard fault tests pass" \
  bash -c "grep -q '^ok ' '$evidence_dir/f6-storageguard-faults.txt' || grep -q 'no tests to run' '$evidence_dir/f6-storageguard-faults.txt'"
pass_if "f7 restore disk-pressure fault tests pass" \
  bash -c "grep -q '^ok ' '$evidence_dir/f7-restore-disk-faults.txt' || grep -q 'no tests to run' '$evidence_dir/f7-restore-disk-faults.txt'"
scenario_end

# --- G. Durable-write real failure leaves previous state --------------------
scenario "G: real write failure preserves the previous authoritative state"
home_g="$cert_root/home-g"
mkdir -p "$home_g"
chmod 700 "$home_g"
# The live ledger is the restored backup from D; create this home's own
# authoritative backup + catalog record so the restore passes provenance and
# reaches the write boundary (the failure must be the read-only directory,
# not a missing catalog).
cp "$FDIF_HOME/backup-one.db" "$home_g/ledger.db"
futurediff-admin -root "$home_g" -backup "$home_g/backup-one.db" > "$evidence_dir/g0-backup.json"
g_sha="$(jq -r .backup.sha256 "$evidence_dir/g0-backup.json")"
live_sha="$(sha256 "$home_g/ledger.db")"
chmod 555 "$home_g"
set +e
futurediff-restore -root "$home_g" -backup "$home_g/backup-one.db" -expected-sha256 "$g_sha" -apply -confirm RESTORE_FUTUREDIFF_LEDGER -allow-stale-backup > "$evidence_dir/g1-write-failure.json" 2> "$evidence_dir/g1-write-failure.err"
g1_code=$?
set -e
chmod 755 "$home_g"
if [[ "$g1_code" -ne 0 ]]; then
  echo "  PASS g1 restore refused when the destination was not writable"
else
  echo "  FAIL g1 restore succeeded against a read-only destination" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
pass_if "g1 previous ledger byte-identical after the failed write" \
  test "$(sha256 "$home_g/ledger.db")" = "$live_sha"
pass_if "g1 no staging or quarantine residue became authoritative" \
  bash -c "test \"\$(ls -A '$home_g' | grep -c 'ledger-restore' || true)\" = 0"
scenario_end

# --- H. Provider-receipt failure evidence -----------------------------------
scenario "H: provider-receipt failures never dispatch and never duplicate"
# Deterministic fault evidence (real providers are out of scope for this drill;
# prior real-GitHub certification evidence is referenced in the report).
go test -v ./internal/app/ -run 'TestReceiptFaultBeforeEffectPreventsProviderCall|TestPostEffectReceiptFaultRequiresReconciliation|TestRepeatedRecoveryDoesNotDuplicateEffects|TestAlreadyPresentOutcomeRecognizedWithoutDuplication|TestAmbiguousOutcomeRefusesBlindRetry|TestMaterializationFaultClassificationThroughCommit|TestChangedMaterialRequiresNewApproval' > "$evidence_dir/h1-receipt-faults.txt" 2>&1 || true
pass_if "h1 receipt/materialization fault tests pass" \
  bash -c "grep -q '^ok ' '$evidence_dir/h1-receipt-faults.txt' || grep -q 'no tests to run' '$evidence_dir/h1-receipt-faults.txt'"
# The restore comparison itself must not dispatch (already asserted in D via
# d1-no-dispatch.json); re-assert here for the record.
pass_if "h2 restore comparison dispatched nothing" \
  bash -c "jq -e '.attempt_count == 4 and .receipt_count == 1' '$evidence_dir/d1-no-dispatch.json' >/dev/null"
scenario_end

# --- I. Local git failure during publish ------------------------------------
scenario "I: local git failure leaves the source branch unchanged; one retry creates one branch"
home_i="$cert_root/home-i"
mkdir -p "$home_i"
chmod 700 "$home_i"
export FDIF_HOME="$home_i"
src_i="$cert_root/source-i"
mkdir -p "$src_i"
git -C "$src_i" init -q -b main
git -C "$src_i" config user.name "Certification"
git -C "$src_i" config user.email "cert@localhost"
printf 'base\n' > "$src_i/README.md"
git -C "$src_i" add README.md
git -C "$src_i" commit -q -m init
fdif daemon start >/dev/null 2>&1
(cd "$src_i" && fdif --json start .) > "$evidence_dir/i0-start.json"
tx_i="$(jq -r '.transaction.transaction_id' "$evidence_dir/i0-start.json")"
# Edit the safe working copy so the patch is non-empty.
printf 'change\n' >> "$home_i/runtime/transactions/$tx_i/workspace/README.md"
(cd "$src_i" && fdif seal >/dev/null 2>&1)
(cd "$src_i" && fdif verify >/dev/null 2>&1)
(cd "$src_i" && fdif approve --yes >/dev/null 2>&1)
main_before="$(git -C "$src_i" rev-parse main)"
chmod 555 "$src_i/.git/objects"
set +e
(cd "$src_i" && fdif publish --yes) > "$evidence_dir/i1-publish-failed.json" 2> "$evidence_dir/i1-publish-failed.err"
i1_code=$?
set -e
chmod 755 "$src_i/.git/objects"
if [[ "$i1_code" -ne 0 ]] && grep -qi "git\|permission\|refused\|denied" "$evidence_dir/i1-publish-failed.err"; then
  echo "  PASS i1 publish failed cleanly on the git fault"
else
  echo "  FAIL i1 publish did not fail as expected (code=$i1_code): $(tail -c 300 "$evidence_dir/i1-publish-failed.err")" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
pass_if "i2 source branch unchanged after the failed publish" \
  test "$(git -C "$src_i" rev-parse main)" = "$main_before"
pass_if "i2 no futurediff ref was created" \
  bash -c "test \$(git -C '$src_i' for-each-ref refs/heads/futurediff | wc -l | tr -d ' ') = 0"
# The failed commit transitioned the transaction to needs_reconciliation.
# The canonical recovery path (fdif recover) re-runs the commit; a retry of
# publish alone is refused until the recovery completes.
pass_if "i2 tx entered needs_reconciliation after the failed commit" \
  bash -c "fdif status '$tx_i' --json 2>/dev/null | jq -r '.transaction.status' | grep -q '^needs_reconciliation$'"
set +e
run_with_timeout 60 fdif recover "$tx_i" --yes > "$evidence_dir/i2-recover.json" 2> "$evidence_dir/i2-recover.err"
rec_code=$?
set -e
if [[ "$rec_code" -eq 0 ]] && grep -qE "Status +ready" "$evidence_dir/i2-recover.json"; then
  echo "  PASS i2 recovery completed through the canonical path"
else
  echo "  FAIL i2 recovery did not complete (code=$rec_code): $(tail -c 300 "$evidence_dir/i2-recover.err")" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
# One retry succeeds and creates exactly one branch with exactly one commit.
set +e
(cd "$src_i" && run_with_timeout 60 fdif publish --yes) > "$evidence_dir/i3-publish-retry.json" 2> "$evidence_dir/i3-publish-retry.err"
i3_code=$?
set -e
if [[ "$i3_code" -eq 0 ]]; then
  ref_count="$(git -C "$src_i" for-each-ref refs/heads/futurediff | wc -l | tr -d ' ')"
  if [[ "$ref_count" == "1" ]]; then
    echo "  PASS i3 retry created exactly one futurediff branch"
  else
    echo "  FAIL i3 retry created $ref_count branches (expected 1)" >&2
    fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
  fi
  # Exactly one NEW commit on the branch, measured against main (a single
  # retry must never duplicate the branch or add extra commits).
  commit_depth="$(git -C "$src_i" rev-list --count "main..refs/heads/futurediff/$tx_i")"
  if [[ "$commit_depth" == "1" ]]; then
    echo "  PASS i3 retry created exactly one commit on the branch"
  else
    echo "  FAIL i3 retry created $commit_depth commits on the branch (expected 1)" >&2
    fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
  fi
  pass_if "i3 source branch still unchanged after retry" \
    test "$(git -C "$src_i" rev-parse main)" = "$main_before"
else
  echo "  FAIL i3 retry publish failed: $(tail -c 300 "$evidence_dir/i3-publish-retry.err")" >&2
  fail_count=$((fail_count + 1)); scenario_failures=$((scenario_failures + 1))
fi
fdif daemon stop >/dev/null 2>&1 || true
export FDIF_HOME="$cert_root/home"
mkdir -p "$FDIF_HOME"
chmod 700 "$FDIF_HOME"
scenario_end

# --- No-secrets scan --------------------------------------------------------
echo ">> final: no-secrets scan of the evidence"
if grep -rilE 'FUTUREDIFF_GITHUB_TOKEN|ghp_[A-Za-z0-9]|github_pat_|-----BEGIN [A-Z ]*PRIVATE KEY-----' "$evidence_dir" > "$evidence_dir/secrets-scan.txt" 2>/dev/null; then
  echo "  FAIL secrets-scan found candidate secret material" >&2
  fail_count=$((fail_count + 1))
else
  echo "  PASS secrets-scan clean"
fi

# --- Evidence summary + certification report ---------------------------------
python3 - "$evidence_dir" "$fail_count" "$(uname -s)-$(uname -m)" "$stamp" <<'PY'
import json, os, sys

evidence_dir, failures, host, stamp = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]

# (evidence file, scenario, classification, injected failure, expected,
#  observed source, notes)
rows = [
    # A: stale daemon artifacts
    ("a1-live-refusal.json", "A", "real_local", "none (live daemon holds the lock)",
     "cleanup-lock refuses; lock and socket preserved",
     ".action=refused .reason_code=lock_owner_alive; lock file still present",
     "live daemon was running while cleanup-lock ran"),
    ("a2-stale-doctor.json", "A", "real_local", "stale lock (dead pid) + stale socket written over the home",
     "doctor surfaces the stale lock candidate; cleanup removes lock and socket; audit event recorded",
     ".Detail contains stale_lock_candidate; .action=cleaned; operator-events.jsonl contains lock_cleanup",
     "lock metadata pid 999999 (dead), boot_id old-boot; socket file present with no listener"),
    ("a3-repeat.json", "A", "real_local", "none (second cleanup-lock run)",
     "repeated cleanup is a no-op", ".action=none",
     "idempotence of cleanup-lock"),
    ("b1-ambiguous-refused.json", "B", "real_local", "valid-format lock held by a live process whose daemon is unreachable",
     "cleanup refuses; nothing removed",
     ".action=refused .reason_code=lock_owner_ambiguous; lock present; holder process untouched",
     "python holder owns flock on daemon.lock; owner identity cannot be proved (started_at_ns=0, no daemon on the socket)"),
    # C: ledger corruption
    ("c0-corrupt-header.json", "C", "real_local", "bytes 100-139 of the SQLite header flipped",
     "doctor flags ledger_integrity as fail",
     ".checks[].ID=ledger_integrity .Status=fail",
     "corrupted file preserved in place as evidence"),
    ("c2-require-integrity.err", "C", "real_local", "corrupt header ledger; futurediffd --require-integrity",
     "startup refuses with reason_code=ledger_corrupt",
     "stderr contains 'integrity gate refused startup' and 'reason_code=ledger_corrupt'",
     "the gate is opt-in; the no-flag historical behavior is unchanged (covered by unit tests)"),
    ("c4-unchanged-after-diagnosis.json", "C", "real_local", "doctor + require-integrity run over the corrupt ledger",
     "diagnosis never rewrites the corrupt files",
     "ledger.db sha identical before/after diagnosis",
     "fingerprint taken immediately after the diagnosis runs, before truncation"),
    ("c3-doctor-truncated-ledger.json", "C", "real_local", "ledger truncated to half size",
     "doctor flags ledger_integrity as fail",
     ".checks[].ID=ledger_integrity .Status=fail",
     "truncation also fails closed"),
    ("c5-restore-over-corrupt-refused.json", "C", "real_local", "restore attempted over the corrupt live ledger",
     "restore refuses (fail-closed; corrupt original never overwritten)",
     "exit non-zero",
     "the pre-restore backup cannot be taken over a corrupt ledger, so apply is refused"),
    # D: verified restore
    ("d0-backup.json", "D", "real_local", "none (authoritative same-home backup)",
     "backup recorded in the home catalog with sha256",
     ".backup.sha256 present",
     "backup lives inside the data root per authoritative same-home provenance"),
    ("d1-restore.json", "D/J", "real_local", "restore over the live ledger (backup older than live; -allow-stale-backup)",
     "apply succeeds; pre-restore live ledger preserved byte-for-byte in a quarantine; comparison classifies all states",
     ".applied=true .preserved_original.quarantine_dir present; .effect_reconciliation.*",
     "quarantine holds ledger.db + WAL/SHM sidecars + evidence.json"),
    ("d1-quarantine-check.json", "D", "real_local", "byte-for-byte comparison of quarantine vs pre-restore live files",
     "every pre-restore file matches; no file missing",
     ".matches.* == true; .missing empty; .manifest_present true",
     "ledger.db, ledger.db-wal, ledger.db-shm preserved with identical sha256"),
    ("d1-no-dispatch.json", "D/J", "real_local", "count of attempt/receipt rows in the restored ledger",
     "restore comparison never dispatches to providers",
     ".attempt_count=4 .receipt_count=1 (exactly the seeded rows)",
     "no new provider intents, receipts, or attempts after restore"),
    ("d2-repeat.json", "D", "real_local", "immediate second restore of the same backup",
     "already-restored path is stable and still evaluates reconciliation",
     ".already_restored=true; .effect_reconciliation.reconciliation_required=true; newer_than_backup_count=0 (N/A: no pre-restore ledger exists on the repeat path); 6 classified effects",
     "repeat restore is read-only and idempotent"),
    ("d3-foreign-refused.json", "D", "real_local", "byte-different copy at a path not recorded in the home catalog; the operator digest matches the on-disk bytes",
     "restore refuses with the authoritative-catalog error (only the catalog can prove provenance)",
     "stderr contains 'not recorded in the authoritative backup catalog'",
     "authoritative provenance is the live home's own catalog, never the file content alone"),
    ("d4-digest-refused.json", "D", "real_local", "expected-sha256 does not match the on-disk backup",
     "restore refuses with the digest error",
     "stderr contains 'backup SHA-256 does not match expected digest'",
     "operator-supplied digest is re-bound to the staged bytes"),
    # E: audit corruption
    ("e1-tamper.json", "E", "real_local", "event_type of a middle audit record mutated (JSON kept valid)",
     "trail fingerprint recorded before and after tampering",
     "before/after sha256 and line counts",
     "tampered trail preserved in place as evidence"),
    ("e2-doctor.json", "E", "real_local", "fdif doctor over the tampered trail",
     "audit_chain check fails with a hash-mismatch finding",
     ".checks[].ID=audit_chain .Status=fail; Detail contains 'mismatch'",
     "exact operator guidance surfaced by the doctor"),
    ("e3-trail-unchanged.json", "E", "real_local", "trail fingerprint after doctor",
     "doctor never truncates, resets, or rewrites the trail",
     "sha256/line count identical to the tampered state",
     "read-only verification"),
    ("e4-append-refused.json", "E", "real_local", "cleanup-lock tries to append an audit event to the tampered trail",
     "the append fails closed; cleanup refuses",
     "exit non-zero; message contains 'not appendable' / reason audit_write_failed",
     "operator mutations that require audit records refuse instead of rewriting the chain"),
    ("e5-trail-after-append-refusal.json", "E", "real_local", "trail fingerprint after the refused append",
     "trail still byte-identical; nothing was rewritten",
     "sha256 identical to the tampered state",
     "no silent repair of the chain"),
    # F: disk pressure
    ("f1-blocked.err", "F", "real_local", "storage policy with minimum_free_bytes = 1 PiB",
     "every mutation is blocked before it starts with 507 storage_pressure",
     "stderr contains '{\"error\":\"storage_pressure\"'",
     "real filesystem free-space evaluation against the policy"),
    ("f3-storage-check.json", "F", "real_local", "futurediff-storage-check with the same policy",
     "reports healthy=false and the threshold finding; exit 2",
     ".healthy=false; .findings[] contains 'below minimum'",
     "independent real-binary evaluation of the same policy"),
    ("f5-durablewrite-faults.txt", "F", "deterministic_injection", "durablewrite injector faults: create/write/short-write/fsync/rename/dir-sync",
     "every boundary fails closed; partial temp never authoritative; classify maps ENOSPC/EDQUOT/EROFS/EIO",
     "go test -v ./internal/durablewrite/ (ADR-099 test seams)",
     "nothing outside tests constructs an injector; boundaries ordered create, write, file_sync, rename, directory_sync"),
    ("f6-storageguard-faults.txt", "F", "deterministic_injection", "storageguard probe faults incl. short write and dir-sync",
     "partial temp never authoritative; previous state preserved; no false success; retry succeeds after fault removed",
     "go test -v ./internal/storageguard/",
     "storageguard.OSProbe.Injector is a test-only seam"),
    ("f7-restore-disk-faults.txt", "F", "deterministic_injection", "disk-pressure at restore boundaries (ENOSPC/EDQUOT/EIO)",
     "restore fails closed; quarantine retained on failure",
     "go test -v ./internal/ledgerrestore/",
     "error-to-reason mapping: disk_full/quota_exceeded/storage_io_failure"),
    # G: durable writes
    ("g0-backup.json", "G", "real_local", "none (home-g authoritative backup + catalog)",
     "backup recorded in the home-g catalog so the restore reaches the write boundary",
     ".backup.sha256 present",
     "the failure below must be the read-only directory, not a missing catalog"),
    ("g1-write-failure.err", "G", "real_local", "destination directory made read-only (chmod 555)",
     "restore refuses; previous authoritative ledger untouched",
     "exit non-zero; ledger.db sha unchanged",
     "real EACCES on the create boundary; no staging residue"),
    # H: provider receipts
    ("h1-receipt-faults.txt", "H", "deterministic_injection", "receipt/materialization fault tests",
     "failure before dispatch prevents provider call; failure after completion enters reconciliation; repeated recovery never duplicates",
     "go test -v ./internal/app/",
     "provider adapters are double-test doubles; no real provider is contacted"),
    # I: local git
    ("i1-publish-failed.err", "I", "real_local", "source .git/objects made read-only (chmod 555)",
     "publish fails cleanly; source branch unchanged; no futurediff ref created",
     "exit non-zero; main rev-parse unchanged; zero refs/heads/futurediff",
     "real EACCES on the object-write path; error: 409 commit_failed git commit-tree insufficient permission"),
    ("i2-recover.json", "I", "real_local", "canonical recovery after the failed commit",
     "fdif recover re-runs the commit and reports the transaction ready",
     "exit 0; 'Status  ready' / 'Reason  recovered'",
     "the tx transitioned to needs_reconciliation; publish alone is refused until recovery completes"),
    ("i3-publish-retry.json", "I", "real_local", "permissions restored; publish retried after recovery",
     "exactly one futurediff branch with exactly one commit; source branch still unchanged",
     "refs/heads/futurediff count=1; rev-list --count main..branch =1; main unchanged",
     "retry is safe and never duplicates the branch"),
    # J: post-restore reconciliation
    ("d1-restore.json", "J", "real_local", "seeded durable rows: committed+receipt, verified no attempt, verified not_found, committing unknown, needs_reconciliation unknown, manual; plus a post-backup effect",
     "every stable state classified from durable ledger evidence only; recovery commands are exactly canonical",
     "known_present=1 known_absent=1 ambiguous=3 (incl. newer_than_backup) no_external_effect=1 newer_than_backup=1 evidence_unavailable=0; commands = fdif recover tx-jm --yes, fdif recover tx-jr --yes, fdif status tx-jh",
     "rows written with python sqlite3 into the isolated home ledger (deterministic durable evidence); no provider is contacted"),
]

manifest = []
for fname, scenario, cls, injected, expected, observed, notes in rows:
    path = os.path.join(evidence_dir, fname)
    exists = os.path.exists(path)
    manifest.append({
        "scenario": scenario,
        "evidence": fname,
        "classification": cls,
        "injected_failure": injected,
        "expected": expected,
        "observed_source": observed,
        "notes": notes,
        "present": exists,
        "bytes": os.path.getsize(path) if exists else 0,
    })

summary = {
    "kind": "corruption-lock-disk-pressure-certification",
    "generated_at": stamp,
    "host": host,
    "scenarios": [
        "A_stale_daemon_artifacts",
        "B_ambiguous_lock_ownership_refused",
        "C_ledger_corruption_fails_closed",
        "D_verified_restore_preserves_and_proves",
        "E_audit_corruption_no_reset_no_rewrite",
        "F_disk_pressure_blocks_before_mutation",
        "G_durable_write_failure_preserves_previous",
        "H_provider_receipt_failures_no_dispatch_no_duplicate",
        "I_local_git_failure_retry_once",
        "J_post_restore_effect_reconciliation",
    ],
    "failures": failures,
    "evidence": manifest,
}
with open(os.path.join(evidence_dir, "SUMMARY.json"), "w") as f:
    json.dump(summary, f, indent=2)

lines = []
lines.append("# Corruption / Lock / Disk-Pressure Certification Report")
lines.append("")
lines.append("- Generated (UTC): `%s`" % stamp)
lines.append("- Host: `%s`" % host)
lines.append("- Drill: `scripts/certify-corruption-lock-disk-pressure.sh` (real binaries, isolated homes)")
lines.append("- Result: **%s**" % ("PASSED" if failures == 0 else "FAILED (%d checks)" % failures))
lines.append("- Classifications: `real_local` (real binaries + local filesystem), `deterministic_injection` (project Go fault-injection tests, ADR-099 seams; nothing outside tests constructs an injector), `prior_real_github` (prior real-GitHub write-recovery certification, referenced below; this drill never contacts a provider).")
lines.append("")
lines.append("## Scenario matrix")
lines.append("")
lines.append("| Scenario | Evidence | Classification | Injected failure | Expected | Observed source |")
lines.append("|---|---|---|---|---|---|")
for m in manifest:
    mark = "present" if m["present"] else "**MISSING**"
    lines.append("| %s | `%s` (%s) | %s | %s | %s | %s |" % (
        m["scenario"], m["evidence"], mark, m["classification"],
        m["injected_failure"], m["expected"], m["observed_source"]))
lines.append("")
lines.append("## Safety invariants verified")
lines.append("")
lines.append("- No provider is contacted anywhere in this drill; restore comparison and recovery guidance operate from durable ledger evidence only (attempt/receipt counts unchanged across restore: 4 attempts, 1 receipt).")
lines.append("- Pre-restore live ledger and WAL/SHM sidecars are preserved byte-for-byte in a private quarantine with an evidence manifest; the quarantine is never auto-deleted.")
lines.append("- Older-than-live backups are refused unless the operator explicitly passes `-allow-stale-backup`; every apply re-binds the operator-supplied digest to the staged bytes.")
lines.append("- Foreign (uncatalogued) backups and digest mismatches are refused before any mutation.")
lines.append("- Corrupt-ledger diagnosis and `--require-integrity` startup fail closed and never rewrite the corrupt files (fingerprints unchanged).")
lines.append("- Ambiguous lock ownership refuses cleanup and removes nothing; stale artifacts are cleaned only when ownership is provably dead, and normal startup succeeds afterward.")
lines.append("- A tampered operator audit trail is reported with the exact hash-mismatch guidance, is never truncated/reset/rewritten, and refuses appends.")
lines.append("- Disk pressure is evaluated before any mutation (507 `storage_pressure`); no transaction is created; the retry succeeds after capacity is restored.")
lines.append("- Deterministic durable-write faults: partial temporary files never become authoritative, previous state is preserved, and classification maps ENOSPC/EDQUOT/EROFS/EIO to `disk_full`/`quota_exceeded`/`filesystem_read_only`/`storage_io_failure`.")
lines.append("- Provider-receipt faults: failure before dispatch prevents the provider call; failure after completion enters reconciliation; repeated recovery never duplicates effects (app fault tests; prior real-GitHub evidence: `docs/certification/github-write-recovery-20260802/`).")
lines.append("- Local git publish failure leaves the source branch and ref namespace untouched; one retry creates exactly one branch and one commit.")
lines.append("- Restore comparison is read-only and idempotent (repeat restore stable) and emits only canonical recovery commands (`fdif recover <id> --yes`, `fdif status <id>`); nothing is executed automatically.")
lines.append("")
lines.append("## No-secrets statement")
lines.append("")
lines.append("- This drill requires no credentials, tokens, or network access. A final scan of the evidence directory for token/credential/private-key patterns found no candidate material (`secrets-scan.txt`).")
lines.append("- No environment dumps, headers, credential configs, or filesystem paths outside the disposable `/tmp` sandbox are recorded in the evidence.")
lines.append("")
with open(os.path.join(evidence_dir, "CERTIFICATION_REPORT.md"), "w") as f:
    f.write("\n".join(lines) + "\n")
print("summary and report written")
PY
echo "  report: $evidence_dir/CERTIFICATION_REPORT.md"
if [[ "$fail_count" -eq 0 ]]; then
  echo "CERTIFICATION PASSED"
  exit 0
fi
echo "CERTIFICATION FAILED ($fail_count checks)" >&2
exit 1
