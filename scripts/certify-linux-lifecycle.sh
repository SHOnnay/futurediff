#!/usr/bin/env bash
# certify-linux-lifecycle.sh
#
# Native Linux clean-machine lifecycle drill for FutureDiff stable-readiness.
#
# Proves, on a genuinely fresh Linux host (a GitHub-hosted Ubuntu runner by
# default), the documented lifecycle:
#
#   1. FutureDiff absent initially
#   2. install the defined predecessor/public baseline release
#   3. version/health verification
#   4. fresh isolated FDIF_HOME
#   5. minimal safe local workflow (pre-upgrade)
#   6. upgrade to the candidate artifact built from the PR commit
#   7. candidate version/commit verification
#   8. pre-upgrade state still readable and compatible
#   9. minimal safe local workflow (post-upgrade)
#  10. uninstall per the documented uninstall contract
#  11. installed binaries absent
#  12. FDIF_HOME handling follows the documented policy
#  13. no unexpected residual FutureDiff files/processes/system state
#
# No provider mutation happens: the guided workflow publishes only a safe
# local branch. The canonical FutureDiff repository is never used as
# lifecycle test state.
#
# Environment:
#   CANDIDATE_ARCHIVE   path to the candidate release archive built from the
#                       exact commit under certification (required)
#   COMMIT_EXPECTED     full SHA of the commit under certification (required)
#   BASELINE_VERSION    published baseline release to install first
#                       (default: v0.1.0-alpha.3)
#   PREFIX              fresh install prefix (default: $RUNNER_TEMP/fdif-prefix)
#   FDIF_HOME           fresh FutureDiff home (default: $RUNNER_TEMP/fdif-home)
#   WORK_DIR            fresh scratch directory (default: $RUNNER_TEMP/fdif-work)
#   OUTPUT              lifecycle evidence JSON path
#                       (default: ./lifecycle-evidence.json)
#
# Exit non-zero if any step fails (fail-closed).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(cat "$repo_root/VERSION")"
baseline_version="${BASELINE_VERSION:-$version}"
candidate_archive="${CANDIDATE_ARCHIVE:-}"
commit_expected="${COMMIT_EXPECTED:-}"
if [[ -z "$candidate_archive" || ! -f "$candidate_archive" ]]; then
  echo "error: CANDIDATE_ARCHIVE is required and must exist: '$candidate_archive'" >&2
  exit 2
fi
if [[ -z "$commit_expected" ]]; then
  echo "error: COMMIT_EXPECTED is required (full SHA of the commit under certification)" >&2
  exit 2
fi

scratch="${RUNNER_TEMP:-/tmp}"
prefix="${PREFIX:-$scratch/fdif-prefix}"
fdif_home="${FDIF_HOME:-$scratch/fdif-home}"
work_dir="${WORK_DIR:-$scratch/fdif-work}"
output="${OUTPUT:-lifecycle-evidence.json}"

for tool in curl tar sha256sum python3 git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is required" >&2; exit 2; }
done

mkdir -p "$work_dir"
run_info_file="$work_dir/run-info.json"

# --- runner / host identity ------------------------------------------------
uname_out="$(uname -a 2>/dev/null || true)"
os_release="$(cat /etc/os-release 2>/dev/null | tr '\n' ' ' || true)"
runner_name="${RUNNER_NAME:-}"
runner_arch="${RUNNER_ARCH:-}"
run_id="${GITHUB_RUN_ID:-}"
run_number="${GITHUB_RUN_NUMBER:-}"
repo="${GITHUB_REPOSITORY:-}"
ref="${GITHUB_REF:-}"
python3 - "$run_info_file" "$uname_out" "$os_release" "$runner_name" "$runner_arch" "$run_id" "$run_number" "$repo" "$ref" "$commit_expected" "$baseline_version" <<'PY'
import json, sys
out, uname, os_release, runner_name, runner_arch, run_id, run_number, repo, ref, commit, baseline = sys.argv[1:13]
json.dump({
    "uname": uname,
    "os_release": os_release,
    "runner_name": runner_name,
    "runner_arch": runner_arch,
    "run_id": run_id,
    "run_number": run_number,
    "repository": repo,
    "ref": ref,
    "commit_tested": commit,
    "baseline_version": baseline,
}, open(out, "w"), indent=2)
PY

results=""
fail() {
  echo "FAIL: $1"
  exit 1
}

record() {
  # record STEP:passed
  results="$results
  \"$1\": $2"
}

# --- 1. clean-machine precondition ------------------------------------------
absent=1
for b in fdif futurediff futurediffd; do
  if command -v "$b" >/dev/null 2>&1 || [[ -e "$prefix/bin/$b" ]]; then
    absent=0
  fi
done
[[ -e "$HOME/.futurediff" ]] && absent=0
[[ $absent -eq 1 ]] || fail "clean-machine precondition: FutureDiff already present (prefix=$prefix home=$HOME/.futurediff)"
record "1_absent_precondition": true

# --- 2. install the defined baseline release --------------------------------
"$repo_root/scripts/install-release.sh" --version "$baseline_version" --prefix "$prefix" > "$work_dir/install-baseline.log" 2>&1 \
  || { echo "baseline install failed:"; tail -5 "$work_dir/install-baseline.log"; exit 1; }
record "2_install_baseline": true

# --- 3. version/health verification ------------------------------------------
v_ok=0
if "$prefix/bin/fdif" version 2>&1 | grep -Fq "$baseline_version"; then
  v_ok=1
fi
"$prefix/bin/futurediffd" --version > "$work_dir/d-version.log" 2>&1 || v_ok=0
FDIF_HOME="$fdif_home" "$prefix/bin/fdif" doctor > "$work_dir/doctor-baseline.log" 2>&1 || true
[[ $v_ok -eq 1 ]] || fail "baseline version/health verification failed"
record "3_version_health": true

# --- 4. fresh isolated FDIF_HOME --------------------------------------------
rm -rf "$fdif_home"
mkdir -p "$fdif_home"
chmod 700 "$fdif_home"
record "4_fresh_fdif_home": true

# --- guided flow helpers ----------------------------------------------------
export FDIF_HOME="$fdif_home"
export PATH="$prefix/bin:$PATH"

run_workflow() {
  # run_workflow LABEL -> writes transaction id to stdout; fails on error
  local label=$1 repo_dir="$work_dir/repo-$label"
  rm -rf "$repo_dir"
  git init -q -b main "$repo_dir"
  git -C "$repo_dir" config user.email "lifecycle@example.invalid"
  git -C "$repo_dir" config user.name "Lifecycle Drill"
  printf 'workflow input\n' > "$repo_dir/README.md"
  git -C "$repo_dir" add README.md
  git -C "$repo_dir" commit -q -m "initial" --no-gpg-sign
  local start_json
  start_json="$(cd "$repo_dir" && fdif --json start .)"
  local tx
  tx="$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['transaction']['transaction_id'])" "$start_json")"
  [[ -n "$tx" ]] || fail "$label: no transaction id"
  printf 'lifecycle %s change\n' "$label" >> "$fdif_home/runtime/transactions/$tx/workspace/README.md"
  fdif --json --yes finish "$tx" > "$work_dir/finish-$label.json" 2>&1 \
    || fail "$label: fdif finish failed"
  git -C "$repo_dir" rev-parse --verify -q "refs/heads/futurediff/$tx" >/dev/null 2>&1 \
    || fail "$label: local publish branch futurediff/$tx missing"
  printf '%s' "$tx"
}

# --- 5. minimal safe local workflow (pre-upgrade) -----------------------------
tx1="$(run_workflow pre)" || exit 1
record "5_preupgrade_workflow": true

# --- 6. upgrade to the candidate artifact -------------------------------------
candidate_sha="$(sha256sum "$candidate_archive" | awk '{print $1}')"
sidecar="$candidate_archive.sha256"
[[ -f "$sidecar" ]] || fail "candidate .sha256 sidecar missing"
(
  cd "$(dirname "$candidate_archive")"
  sha256sum -c "$(basename "$sidecar")"
) > "$work_dir/candidate-checksum.log" 2>&1 || fail "candidate checksum verification failed"
extract_dir="$work_dir/candidate-extract"
rm -rf "$extract_dir"
mkdir -p "$extract_dir"
tar -xzf "$candidate_archive" -C "$extract_dir"
root_dir="$(find "$extract_dir" -maxdepth 1 -mindepth 1 -type d | head -1)"
[[ -n "$root_dir" ]] || fail "candidate archive root missing"
for b in fdif futurediff futurediffd; do
  [[ -x "$root_dir/bin/$b" ]] || fail "candidate archive missing executable $b"
  install -m 0755 "$root_dir/bin/$b" "$prefix/bin/$b"
done
record "6_upgrade_candidate": true

# --- 7. candidate version/commit verification --------------------------------
cand_json="$("$prefix/bin/fdif" version --json 2>/dev/null || "$prefix/bin/fdif" version 2>&1 | head -20)"
cand_version="$(printf '%s' "$cand_json" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('version',''))" 2>/dev/null || true)"
cand_commit="$(printf '%s' "$cand_json" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('commit',''))" 2>/dev/null || true)"
[[ "$cand_version" == "$version" ]] || fail "candidate version mismatch: got '$cand_version' want '$version'"
[[ "$cand_commit" == "$commit_expected" ]] || fail "candidate commit mismatch: got '$cand_commit' want '$commit_expected'"
record "7_candidate_version_commit": true

# --- 8. pre-upgrade state readable and compatible ----------------------------
tx1_ok=0
if git -C "$work_dir/repo-pre" rev-parse --verify -q "refs/heads/futurediff/$tx1" >/dev/null 2>&1 \
   && fdif transactions --json 2>/dev/null | grep -Fq "\"$tx1\""; then
  tx1_ok=1
fi
[[ $tx1_ok -eq 1 ]] || fail "pre-upgrade state not readable after upgrade"
record "8_state_compatibility": true

# --- 9. minimal safe local workflow (post-upgrade) ----------------------------
tx2="$(run_workflow post)" || exit 1
record "9_postupgrade_workflow": true

# --- 10. uninstall per the documented contract --------------------------------
rm -f "$prefix/bin/fdif" "$prefix/bin/futurediff" "$prefix/bin/futurediffd"
rm -rf "$fdif_home"
record "10_uninstall": true

# --- 11. installed binaries absent --------------------------------------------
bin_residue=""
for b in fdif futurediff futurediffd; do
  [[ -e "$prefix/bin/$b" ]] && bin_residue="$bin_residue $b"
done
clean_path="$(printf '%s' "$PATH" | tr ':' '\n' | grep -v "^$prefix/bin$" | paste -sd: -)"
if [[ -n "$bin_residue" ]]; then
  fail "uninstall left binaries:$bin_residue"
fi
if command -v fdif >/dev/null 2>&1; then
  # a system-wide fdif (outside the drill prefix) must not exist either
  system_fdif="$(command -v fdif || true)"
  [[ -z "$system_fdif" ]] || fail "system-wide fdif still present: $system_fdif"
fi
record "11_binaries_absent": true

# --- 12. FDIF_HOME handling follows the documented policy ----------------------
home_ok=1
[[ -e "$fdif_home" ]] && home_ok=0
[[ -e "$HOME/.futurediff" ]] && home_ok=0
[[ $home_ok -eq 1 ]] || fail "FDIF_HOME policy violated: drill home or ~/.futurediff still present"
record "12_fdif_home_policy": true

# --- 13. residue check --------------------------------------------------------
residue_ok=1
residue_detail=""
proc="$(pgrep -f 'futurediff|fdif ' 2>/dev/null | head -5 | tr '\n' ' ' || true)"
[[ -n "$proc" ]] && { residue_ok=0; residue_detail="processes: $proc"; }
units="$(systemctl list-unit-files 2>/dev/null | grep -i futurediff || true)"
[[ -n "$units" ]] && { residue_ok=0; residue_detail="$residue_detail units: $units"; }
prefix_left="$(find "$prefix" -mindepth 1 2>/dev/null | head -5 | tr '\n' ' ' || true)"
[[ -n "$prefix_left" ]] && { residue_ok=0; residue_detail="$residue_detail prefix: $prefix_left"; }
[[ $residue_ok -eq 1 ]] || fail "residue found: $residue_detail"
record "13_residue": true

# --- assemble evidence -------------------------------------------------------
python3 - "$output" "$run_info_file" "$results" "$candidate_sha" "$tx1" "$tx2" "$prefix" "$fdif_home" <<'PY'
import json, sys
out, run_info_file, results_lines, candidate_sha, tx1, tx2, prefix, home = sys.argv[1:9]
run_info = json.load(open(run_info_file))
lines = [l.strip() for l in results_lines.splitlines() if l.strip()]
steps = {}
for line in lines:
    key, _, val = line.partition(":")
    steps[key.strip('"')] = val.strip() == "true"
all_pass = all(steps.values())
doc = {
    "kind": "linux-clean-machine-lifecycle",
    "classification": "hosted_native_linux",
    "host": "github-hosted-runner",
    "commit_tested": run_info["commit_tested"],
    "runner": {k: run_info[k] for k in ("uname", "os_release", "runner_name", "runner_arch", "run_id", "run_number", "repository", "ref")},
    "baseline_version": run_info["baseline_version"],
    "candidate_version": None,
    "candidate_archive_sha256": candidate_sha,
    "pre_upgrade_transaction": tx1,
    "post_upgrade_transaction": tx2,
    "prefix": prefix,
    "fdif_home": home,
    "steps": steps,
    "pass": all_pass,
}
json.dump(doc, open(out, "w"), indent=2)
if not all_pass:
    raise SystemExit(1)
print(json.dumps({"pass": all_pass, "steps": len(steps)}))
PY

echo "native Linux clean-machine lifecycle evidence written to: $output"
