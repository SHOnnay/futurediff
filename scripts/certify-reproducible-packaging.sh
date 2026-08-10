#!/usr/bin/env bash
# certify-reproducible-packaging.sh
#
# Reproducible packaging certification for FutureDiff stable-readiness:
#
#   1. byte-for-byte reproducible release archives for the native target
#      (build A and B in fresh temporary directories; SOURCE_DATE_EPOCH
#      derived from the committed source tree)
#   2. regression/negative tests proving determinism is real
#   3. re-verification of the normalized archive with the existing
#      supply-chain tooling (checksum sidecar, ephemeral sign/verify,
#      tamper negatives, SBOM, provenance, source-zip release-verify)
#   4. ingestion of hosted native-Linux lifecycle + per-target
#      reproducibility evidence produced by CI (--hosted-evidence)
#
# Evidence classes (recorded per artifact in SUMMARY.json):
#   deterministic_local            local, byte-verifiable checks
#   hosted_native_linux            evidence produced by the
#                                  lifecycle-certification CI workflow on a
#                                  fresh native runner (never invented locally)
#   blocked_external_prerequisite  hosted/external items with exact prerequisite
#   not_applicable                 documented unsupported targets (Windows)
#
# Usage:
#   scripts/certify-reproducible-packaging.sh \
#     --confirm I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY \
#     [--nonce RUN_NONCE] [--evidence-dir DIR] [--hosted-evidence DIR] \
#     [--phase reproducibility-only] [--skip-build] [--keep-worktree]
#
# --phase reproducibility-only runs phases 1-2 and writes a self-contained
# per-target evidence set (used by the CI matrix on each native runner).
#
# The confirmation phrase is
#   I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
assurance="python3 $repo_root/tools/futurediff_assurance.py"

CONFIRMATION_PHRASE="I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY"
nonce=""
evidence_dir=""
hosted_evidence_dir=""
phase="full"
confirm_phrase=""
skip_build=0
keep_worktree=0
version="$(cat "$repo_root/VERSION")"
repo="SHOnnay/futurediff"
builder_id="https://github.com/SHOnnay/futurediff/actions"
source_uri="git+https://github.com/SHOnnay/futurediff"

usage() {
  sed -n '2,44p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --confirm) confirm_phrase=${2:?--confirm requires a value}; shift 2 ;;
    --nonce) nonce=${2:?--nonce requires a value}; shift 2 ;;
    --evidence-dir) evidence_dir=${2:?--evidence-dir requires a value}; shift 2 ;;
    --hosted-evidence) hosted_evidence_dir=${2:?--hosted-evidence requires a value}; shift 2 ;;
    --phase) phase=${2:?--phase requires a value}; shift 2 ;;
    --skip-build) skip_build=1; shift ;;
    --keep-worktree) keep_worktree=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$confirm_phrase" != "$CONFIRMATION_PHRASE" ]]; then
  echo "error: this certification creates disposable evidence only." >&2
  echo "pass --confirm '$CONFIRMATION_PHRASE' to proceed" >&2
  exit 2
fi

if [[ "$phase" != "full" && "$phase" != "reproducibility-only" ]]; then
  echo "error: --phase must be 'full' or 'reproducibility-only'" >&2
  exit 2
fi

if [[ -z "$nonce" ]]; then
  nonce="$(date +%Y%m%d-%H%M%S)-$RANDOM"
fi
if [[ -z "$evidence_dir" ]]; then
  evidence_dir="$repo_root/docs/certification/reproducible-packaging-linux-lifecycle-$nonce"
fi

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 2; }
}
require go
require python3
require git
require openssl
require tar
require gzip
require shasum
require curl

mkdir -p "$evidence_dir/deterministic_local" "$evidence_dir/hosted_native_linux"
echo "evidence directory: $evidence_dir"

host="$(uname -s)-$(uname -m)"
os="$(uname -s)"
git_sha="$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || echo unknown)"
summary_file="$evidence_dir/SUMMARY.json"

cert_root="$(mktemp -d "${TMPDIR:-/tmp}/futurediff-reproducible-cert.XXXXXX")"
pristine="$cert_root/pristine"

cleanup() {
  if [[ $keep_worktree -eq 0 ]]; then
    git -C "$repo_root" worktree remove --force "$pristine" >/dev/null 2>&1 || true
  fi
  rm -rf "$cert_root"
}
trap cleanup EXIT

det_failures=0
det_evidence=()
hosted_evidence=()
blocked_evidence=()
na_evidence=()

emit_det() { det_evidence+=("$(python3 - "$@" <<'PY'
import json, sys
keys = ["id", "classification", "target", "expected", "observed_source", "pass"]
values = sys.argv[1:1 + len(keys)]
d = {k: v for k, v in zip(keys, values) if v != "<none>"}
if "pass" in d:
    d["pass"] = d["pass"] == "True"
print(json.dumps(d, sort_keys=True))
PY
)"); }
emit_hosted() { hosted_evidence+=("$(python3 - "$@" <<'PY'
import json, sys
keys = ["id", "classification", "target", "expected", "observed_source", "pass"]
values = sys.argv[1:1 + len(keys)]
d = {k: v for k, v in zip(keys, values) if v != "<none>"}
if "pass" in d:
    d["pass"] = d["pass"] == "True"
print(json.dumps(d, sort_keys=True))
PY
)"); }
emit_blocked() { blocked_evidence+=("$(python3 - "$@" <<'PY'
import json, sys
keys = ["id", "classification", "target", "expected", "observed_source", "pass", "prerequisite"]
values = sys.argv[1:1 + len(keys)]
d = {k: v for k, v in zip(keys, values) if v != "<none>"}
if "pass" in d:
    d["pass"] = d["pass"] == "True"
print(json.dumps(d, sort_keys=True))
PY
)"); }
emit_na() { na_evidence+=("$(python3 - "$@" <<'PY'
import json, sys
keys = ["id", "classification", "target", "expected", "observed_source", "pass", "note"]
values = sys.argv[1:1 + len(keys)]
d = {k: v for k, v in zip(keys, values) if v != "<none>"}
if "pass" in d:
    d["pass"] = d["pass"] == "True"
print(json.dumps(d, sort_keys=True))
PY
)"); }

git -C "$repo_root" worktree add --detach "$pristine" HEAD >/dev/null 2>&1
pristine_sha="$(git -C "$pristine" rev-parse HEAD)"
target="$(cd "$pristine" && bash scripts/build-public-release.sh --print-target)"
sde="$(git -C "$repo_root" log -1 --format=%ct HEAD)"
sde_iso="$(TZ=UTC date -u -r "$sde" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || TZ=UTC date -u -d "@$sde" +%Y-%m-%dT%H:%M:%SZ)"
echo ">> pristine worktree: $pristine ($pristine_sha)"
echo ">> target: $target | SOURCE_DATE_EPOCH: $sde ($sde_iso)"

platform=${target%-*}
arch=${target#*-}
archive_name="futurediff-$version-$platform-$arch.tar.gz"

# ---------------------------------------------------------------------------
# Phase 1: byte-for-byte reproducibility for the native target
# ---------------------------------------------------------------------------
if [[ $skip_build -eq 0 ]]; then
  echo ">> reproducibility: build A and B in fresh temporary directories"
  ( cd "$pristine" && bash scripts/build-public-release.sh "$version" "$cert_root/buildA" ) > "$cert_root/buildA.log" 2>&1
  ( cd "$pristine" && bash scripts/build-public-release.sh "$version" "$cert_root/buildB" ) > "$cert_root/buildB.log" 2>&1
  # Wall-clock independence: rebuild with a far-offset TZ; SDE is git-derived.
  ( cd "$pristine" && TZ=Etc/GMT-14 bash scripts/build-public-release.sh "$version" "$cert_root/buildC" ) > "$cert_root/buildC.log" 2>&1
  # Changed-source negative: build from a second worktree at HEAD whose only
  # difference is the packaged README.md content.
  git -C "$repo_root" worktree add --detach "$cert_root/mutated" HEAD >/dev/null 2>&1
  printf 'reproducibility negative-test marker\n' >> "$cert_root/mutated/README.md"
  ( cd "$cert_root/mutated" && bash scripts/build-public-release.sh "$version" "$cert_root/buildD" ) > "$cert_root/buildD.log" 2>&1
fi

a1="$cert_root/buildA/$archive_name"
a2="$cert_root/buildB/$archive_name"
a3="$cert_root/buildC/$archive_name"
a4="$cert_root/buildD/$archive_name"

sha1="$(shasum -a 256 "$a1" | awk '{print $1}')"
sha2="$(shasum -a 256 "$a2" | awk '{print $1}')"
sha3="$(shasum -a 256 "$a3" | awk '{print $1}')"
sha4="$(shasum -a 256 "$a4" | awk '{print $1}')"

mkdir -p "$cert_root/px1" "$cert_root/px2"
tar -xzf "$a1" -C "$cert_root/px1"
tar -xzf "$a2" -C "$cert_root/px2"
p1="$(cd "$cert_root/px1" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
p2="$(cd "$cert_root/px2" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"

tar -tvf "$a1" > "$evidence_dir/deterministic_local/archive-A.tar-tvf.txt" 2>&1
tar -tvf "$a2" > "$evidence_dir/deterministic_local/archive-B.tar-tvf.txt" 2>&1
tar -tzf "$a1" | LC_ALL=C sort > "$evidence_dir/deterministic_local/archive-A.entries.txt"
tar -tzf "$a2" | LC_ALL=C sort > "$evidence_dir/deterministic_local/archive-B.entries.txt"

# metadata: uid/gid/uname/gname normalized to 0/root, mtime == SDE
tvf_owner_ok=1
if grep -Evq "^[d-][rwx-]{9} +0 (root|wheel) +(root|wheel) " "$evidence_dir/deterministic_local/archive-A.tar-tvf.txt"; then
  tvf_owner_ok=0
fi
gzip_mtime="$(python3 -c "
import struct
h=open('$a1','rb').read(8)
print(struct.unpack('<I', h[4:8])[0])
")"
gzip_ok=0
[[ "$gzip_mtime" == "$sde" ]] && gzip_ok=1
buildinfo_date="$("$cert_root/px1/futurediff-$version-$platform-$arch/bin/fdif" version --json 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('date',''))" 2>/dev/null || true)"
buildinfo_ok=0
[[ "$buildinfo_date" == "$sde_iso" ]] && buildinfo_ok=1

# build D payload must differ (source change is real, not hidden by normalization)
p4dir="$cert_root/px4"
mkdir -p "$p4dir"
tar -xzf "$a4" -C "$p4dir"
p4="$(cd "$p4dir" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"

python3 - "$evidence_dir/deterministic_local/reproducible-evidence-$target.json" \
  "$target" "$git_sha" "$sde" "$sde_iso" "$sha1" "$sha2" "$sha3" "$sha4" "$p1" "$p2" "$p4" \
  "$tvf_owner_ok" "$gzip_ok" "$buildinfo_ok" "$version" <<'PY'
import json, sys
out, target, commit, sde, sde_iso, sha1, sha2, sha3, sha4, p1, p2, p4, owner_ok, gzip_ok, date_ok, version = sys.argv[1:17]
owner_ok = int(owner_ok) == 1
gzip_ok = int(gzip_ok) == 1
date_ok = int(date_ok) == 1
payload_identical = p1 == p2
archive_identical = sha1 == sha2
wall_clock_independent = sha1 == sha3
source_change_changes_digest = sha4 != sha1
normalization_does_not_hide_payload = (p4 != p1) and source_change_changes_digest
pass_all = payload_identical and archive_identical and owner_ok and gzip_ok and date_ok \
           and wall_clock_independent and source_change_changes_digest and normalization_does_not_hide_payload
doc = {
    "kind": "reproducible-packaging",
    "classification": "deterministic_local",
    "commit_tested": commit,
    "source_date_epoch": int(sde),
    "source_date_epoch_iso": sde_iso,
    "target": target,
    "version": version,
    "archive_sha256_A": sha1,
    "archive_sha256_B": sha2,
    "archive_sha256_TZ_offset": sha3,
    "archive_sha256_changed_source": sha4,
    "payload_sha256_A": p1,
    "payload_sha256_B": p2,
    "payload_sha256_changed_source": p4,
    "payload_identical": payload_identical,
    "archive_identical": archive_identical,
    "archive_metadata_owner_normalized": owner_ok,
    "archive_gzip_mtime_equals_sde": gzip_ok,
    "buildinfo_date_equals_commit_date": date_ok,
    "wall_clock_independent": wall_clock_independent,
    "source_change_changes_digest": source_change_changes_digest,
    "normalization_does_not_hide_payload": normalization_does_not_hide_payload,
    "pass": pass_all,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps({k: doc[k] for k in ("target", "payload_identical", "archive_identical", "wall_clock_independent", "source_change_changes_digest", "normalization_does_not_hide_payload", "pass")}))
PY

rep_json="$evidence_dir/deterministic_local/reproducible-evidence-$target.json"
python3 -c "import json,sys; d=json.load(open('$rep_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det packaged_reproducible deterministic_local reproducible_builds "byte-identical packaged archive for $target (build A == build B)" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;print(json.load(open('$rep_json'))['pass'])")"
emit_det archive_metadata_normalized deterministic_local reproducible_builds "owner/group root:0, tar+gzip mtime == SOURCE_DATE_EPOCH, buildinfo date == commit date" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;d=json.load(open('$rep_json'));print(d['archive_metadata_owner_normalized'] and d['archive_gzip_mtime_equals_sde'] and d['buildinfo_date_equals_commit_date'])")"
emit_det negative_wall_clock_independent deterministic_local reproducible_builds "TZ-offset rebuild is byte-identical" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;print(json.load(open('$rep_json'))['wall_clock_independent'])")"
emit_det negative_source_change_changes_digest deterministic_local reproducible_builds "changed source changes archive digest" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;print(json.load(open('$rep_json'))['source_change_changes_digest'])")"
emit_det negative_normalization_does_not_hide_payload deterministic_local reproducible_builds "metadata normalization cannot hide changed payload bytes" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;print(json.load(open('$rep_json'))['normalization_does_not_hide_payload'])")"
emit_det negative_temp_dir_independent deterministic_local reproducible_builds "fresh temporary build directories do not alter bytes" "deterministic_local/reproducible-evidence-$target.json" "$(python3 -c "import json;print(json.load(open('$rep_json'))['archive_identical'])")"

# ---------------------------------------------------------------------------
# Phase 2: release integrity re-verification on the normalized archive
# ---------------------------------------------------------------------------
echo ">> release integrity: checksum sidecar, ephemeral sign/verify, SBOM, provenance, source zip"

ck_rc=0
set +e
( cd "$cert_root/buildB" && shasum -a 256 -c "$(basename "$a2").sha256" ) > "$evidence_dir/deterministic_local/checksum-verify.txt" 2>&1
ck_rc=$?
set -e
[[ $ck_rc -eq 0 ]] || det_failures=$((det_failures + 1))
emit_det checksum_verify deterministic_local release_checksums "normalized archive .sha256 sidecar verifies" deterministic_local/checksum-verify.txt "$([[ $ck_rc -eq 0 ]] && echo True || echo False)"

mkdir -p "$cert_root/keys"
openssl genrsa -out "$cert_root/keys/release.key" 3072 2>/dev/null
openssl rsa -in "$cert_root/keys/release.key" -pubout -out "$cert_root/keys/release.pub" 2>/dev/null
cp "$cert_root/keys/release.pub" "$evidence_dir/deterministic_local/release-public-key.pem"
$assurance sign --file "$a2" --private-key "$cert_root/keys/release.key" \
  --signature "$evidence_dir/deterministic_local/release-darwin.tar.gz.sig" \
  --output "$evidence_dir/deterministic_local/sign.json" > "$cert_root/sign.log" 2>&1
$assurance verify-signature --file "$a2" --public-key "$cert_root/keys/release.pub" \
  --signature "$evidence_dir/deterministic_local/release-darwin.tar.gz.sig" \
  --output "$evidence_dir/deterministic_local/verify.json" > "$cert_root/verify.log" 2>&1

# tamper negative: a single flipped byte must fail verification
cp "$a2" "$cert_root/tampered.tar.gz"
python3 - "$cert_root/tampered.tar.gz" <<'PY'
import sys
p = sys.argv[1]
data = bytearray(open(p, "rb").read())
data[len(data) // 2] ^= 0x01
open(p, "wb").write(bytes(data))
PY
tamper_rc=0
set +e
$assurance verify-signature --file "$cert_root/tampered.tar.gz" --public-key "$cert_root/keys/release.pub" \
  --signature "$evidence_dir/deterministic_local/release-darwin.tar.gz.sig" \
  --output "$cert_root/tamper-verify.json" > "$cert_root/tamper.log" 2>&1
tamper_rc=$?
set -e

# stale-checksum negative: a changed archive cannot reuse the old sidecar
stale_rc=0
set +e
( cd "$cert_root" && cp tampered.tar.gz changed.tar.gz && printf '%s  changed.tar.gz\n' "$sha2" > changed.tar.gz.sha256 && shasum -a 256 -c changed.tar.gz.sha256 ) > "$cert_root/stale-checksum.log" 2>&1
stale_rc=$?
set -e

python3 - "$evidence_dir/deterministic_local/signature-tamper-evidence.json" \
  "$evidence_dir/deterministic_local/verify.json" "$tamper_rc" "$stale_rc" <<'PY'
import json, sys
out, verify_path, tamper_rc, stale_rc = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
verified = json.load(open(verify_path)).get("verified") is True
tamper_rejected = tamper_rc != 0
stale_checksum_rejected = stale_rc != 0
doc = {
    "classification": "deterministic_local",
    "ephemeral_key": "RSA-3072 generated and destroyed in-run; public key persisted as deterministic_local/release-public-key.pem",
    "normalized_archive_verified": verified,
    "tamper_rejected": tamper_rejected,
    "changed_archive_reuses_no_old_checksum": stale_checksum_rejected,
    "pass": verified and tamper_rejected and stale_checksum_rejected,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY

sig_json="$evidence_dir/deterministic_local/signature-tamper-evidence.json"
python3 -c "import json,sys; d=json.load(open('$sig_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det normalized_archive_sign_verify deterministic_local signed_release_artifacts "ephemeral sign/verify on the normalized archive" deterministic_local/signature-tamper-evidence.json "$(python3 -c "import json;print(json.load(open('$sig_json'))['pass'])")"
emit_det tamper_negative deterministic_local signed_release_artifacts "flipped byte fails verification" deterministic_local/signature-tamper-evidence.json "$(python3 -c "import json;print(json.load(open('$sig_json'))['tamper_rejected'])")"
emit_det stale_checksum_negative deterministic_local release_checksums "changed archive cannot reuse an old checksum/signature" deterministic_local/signature-tamper-evidence.json "$(python3 -c "import json;print(json.load(open('$sig_json'))['changed_archive_reuses_no_old_checksum'])")"

$assurance sbom-create --root "$pristine" --name futurediff --version "$version" \
  --output "$evidence_dir/deterministic_local/sbom.cdx.json" > "$cert_root/sbom-create.log" 2>&1
$assurance sbom-verify --root "$pristine" --sbom "$evidence_dir/deterministic_local/sbom.cdx.json" \
  --output "$evidence_dir/deterministic_local/sbom-verify.json" > "$cert_root/sbom-verify.log" 2>&1
sbom_pass="$(python3 -c "import json;print(json.load(open('$evidence_dir/deterministic_local/sbom-verify.json')).get('verified') is True)")"
emit_det sbom_valid deterministic_local sbom_assets "CycloneDX SBOM create/verify bound to the committed tree" deterministic_local/sbom-verify.json "$sbom_pass"
[[ "$sbom_pass" == "True" ]] || det_failures=$((det_failures + 1))

$assurance provenance-create --root "$pristine" --name futurediff --version "$version" \
  --source-digest "$git_sha" \
  --output "$evidence_dir/deterministic_local/provenance.intoto.json" > "$cert_root/prov-create.log" 2>&1
$assurance provenance-verify --provenance "$evidence_dir/deterministic_local/provenance.intoto.json" \
  --source-digest "$git_sha" \
  --output "$evidence_dir/deterministic_local/provenance-verify.json" > "$cert_root/prov-verify.log" 2>&1
prov_pass="$(python3 -c "import json;d=json.load(open('$evidence_dir/deterministic_local/provenance-verify.json'));print(d.get('verified') is True and d.get('subject_match') is True)")"
emit_det provenance_bound deterministic_local in_toto_provenance "in-toto provenance bound to the committed tree ($git_sha)" deterministic_local/provenance-verify.json "$prov_pass"
[[ "$prov_pass" == "True" ]] || det_failures=$((det_failures + 1))

$assurance release-build --root "$pristine" --name futurediff --version "$version" \
  --output "$cert_root/source.zip" > "$cert_root/rel.json" 2>&1
rv_rc=0
set +e
$assurance release-verify --archive "$cert_root/source.zip" \
  --output "$evidence_dir/deterministic_local/release-verify.json" > "$cert_root/rv.log" 2>&1
rv_rc=$?
set -e
[[ $rv_rc -eq 0 ]] || det_failures=$((det_failures + 1))
emit_det release_verifier deterministic_local release_verification "deterministic source zip release-verify on the committed tree" deterministic_local/release-verify.json "$([[ $rv_rc -eq 0 ]] && echo True || echo False)"

# ---------------------------------------------------------------------------
# Phase 3: hosted native-Linux lifecycle evidence (never invented locally)
# ---------------------------------------------------------------------------
if [[ "$phase" == "full" ]]; then
  if [[ -n "$hosted_evidence_dir" && -d "$hosted_evidence_dir" ]]; then
    echo ">> hosted evidence ingestion: $hosted_evidence_dir"
    hosted_ok=1
    hosted_detail=""
    lifecycle_files=()
    rep_files=()
    while IFS= read -r -d '' f; do
      case "$f" in
        *lifecycle-evidence.json) lifecycle_files+=("$f") ;;
        *reproducible-evidence-*.json) rep_files+=("$f") ;;
      esac
    done < <(find "$hosted_evidence_dir" -type f -name '*.json' -print0)

    if [[ ${#lifecycle_files[@]} -eq 0 ]]; then
      hosted_ok=0; hosted_detail="no lifecycle-evidence.json found in $hosted_evidence_dir"
    else
      lifecycle_file="${lifecycle_files[0]}"
      cp "$lifecycle_file" "$evidence_dir/hosted_native_linux/lifecycle-evidence.json"
      lf_commit="$(python3 -c "import json;print(json.load(open('$evidence_dir/hosted_native_linux/lifecycle-evidence.json'))['commit_tested'])")"
      lf_pass="$(python3 -c "import json;print(json.load(open('$evidence_dir/hosted_native_linux/lifecycle-evidence.json'))['pass'])")"
      if [[ "$lf_commit" != "$git_sha" ]]; then
        hosted_ok=0; hosted_detail="lifecycle evidence commit $lf_commit != $git_sha"
      fi
      if [[ "$lf_pass" != "True" ]]; then
        hosted_ok=0; hosted_detail="$hosted_detail lifecycle evidence pass != True"
      fi
      runner_desc="$(python3 -c "
import json
d=json.load(open('$evidence_dir/hosted_native_linux/lifecycle-evidence.json'))
r=d.get('runner', {})
print('%s %s run=%s' % (r.get('runner_name',''), r.get('runner_arch',''), r.get('run_id','')))
")"
    fi

    target_shas=()
    for f in "${rep_files[@]}"; do
      tgt="$(basename "$f" .json | sed 's/^reproducible-evidence-//')"
      cp "$f" "$evidence_dir/hosted_native_linux/reproducible-evidence-$tgt.json"
      f_commit="$(python3 -c "import json;print(json.load(open('$evidence_dir/hosted_native_linux/reproducible-evidence-$tgt.json'))['commit_tested'])")"
      f_pass="$(python3 -c "import json;print(json.load(open('$evidence_dir/hosted_native_linux/reproducible-evidence-$tgt.json'))['pass'])")"
      f_sha="$(python3 -c "import json;print(json.load(open('$evidence_dir/hosted_native_linux/reproducible-evidence-$tgt.json'))['archive_sha256_A'])")"
      if [[ "$f_commit" != "$git_sha" ]]; then
        hosted_ok=0; hosted_detail="$hosted_detail ${tgt}:commit mismatch"
      fi
      if [[ "$f_pass" != "True" ]]; then
        hosted_ok=0; hosted_detail="$hosted_detail ${tgt}:pass != True"
      fi
      target_shas+=("$tgt:$f_sha")
      emit_hosted hosted_reproducible_${tgt//-/_} hosted_native_linux reproducible_builds "byte-identical packaged archive for $tgt on its native runner" "hosted_native_linux/reproducible-evidence-$tgt.json" "$f_pass"
    done

    # cross-target identity: every target archive must be distinct
    distinct=$(python3 - "$evidence_dir/hosted_native_linux" <<'PY'
import json, sys, pathlib
base = pathlib.Path(sys.argv[1])
shas = []
for f in sorted(base.glob("reproducible-evidence-*.json")):
    d = json.loads(f.read_text())
    shas.append((f.name.replace("reproducible-evidence-", "").replace(".json", ""), d["archive_sha256_A"]))
names = [s[0] for s in shas]
vals = [s[1] for s in shas]
print(len(set(vals)) == len(vals) and len(vals) >= 2)
PY
)
    if [[ "$distinct" != "True" ]]; then
      hosted_ok=0; hosted_detail="$hosted_detail cross-target archive identity not distinct"
    fi
    emit_hosted cross_target_archive_identity hosted_native_linux reproducible_builds "each supported target archive is distinct and reproducible" "hosted_native_linux" "$distinct"

    if [[ $hosted_ok -eq 1 ]]; then
      emit_hosted native_linux_clean_machine_lifecycle hosted_native_linux clean_machine_install "install -> execute -> upgrade -> preserve state -> uninstall on a fresh native Linux runner ($runner_desc)" hosted_native_linux/lifecycle-evidence.json True
    else
      emit_hosted native_linux_clean_machine_lifecycle hosted_native_linux clean_machine_install "install -> execute -> upgrade -> preserve state -> uninstall on a fresh native Linux runner" hosted_native_linux/lifecycle-evidence.json False
      echo "error: hosted evidence failed validation: $hosted_detail" >&2
      exit 1
    fi
  else
    emit_blocked native_linux_clean_machine_lifecycle blocked_external_prerequisite clean_machine_install "clean-machine install/upgrade/uninstall evidence on native Linux" "<none>" False "run the lifecycle-certification CI workflow on a fresh GitHub-hosted Ubuntu runner (not a container) against the candidate built from this commit and pass its lifecycle-evidence.json artifact; hosted evidence is never invented locally"
    emit_blocked hosted_per_target_reproducibility blocked_external_prerequisite reproducible_builds "per-target reproducibility evidence for linux-amd64, linux-arm64, darwin-amd64, darwin-arm64" "<none>" False "run the lifecycle-certification CI matrix on the native runner for each target and pass its reproducible-evidence-<target>.json artifact"
  fi

  emit_na windows_native_lifecycle not_applicable unsupported_target "native Windows clean-machine lifecycle" "<none>" True "Windows runtime/installer support is deferred and unsupported (STABLE_READINESS.md, COMPATIBILITY_AND_DEPRECATION_POLICY.md); not a stable-v1 blocker"
fi

# ---------------------------------------------------------------------------
# Phase 4: report, secret scan, SUMMARY assembly
# ---------------------------------------------------------------------------
cat > "$evidence_dir/CERTIFICATION_REPORT.md" <<EOF
# Reproducible Packaging + Linux Clean-Machine Lifecycle Certification Report

- **Date**: $(date -u +%Y-%m-%dT%H:%M:%SZ)
- **Host**: $host
- **Nonce**: $nonce
- **Git commit**: $git_sha
- **Version**: $version
- **Target**: $target
- **SOURCE_DATE_EPOCH**: $sde ($sde_iso) derived from the committed source tree
- **Confirmation**: $CONFIRMATION_PHRASE

## Evidence classes

- **deterministic_local**: build A/B in fresh temporary directories
  (byte-identical archives), archive metadata normalization checks
  (root:0 owner, tar/gzip mtime == SOURCE_DATE_EPOCH, buildinfo date ==
  commit date), negative tests (TZ offset, changed source, temp dir
  independence, normalization-cannot-hide-payload), and re-verification of
  the normalized archive with the existing supply-chain tooling
  (checksum sidecar, ephemeral sign/verify, tamper negative, stale
  checksum negative, SBOM, provenance, source-zip release-verify).
- **hosted_native_linux**: evidence produced by the lifecycle-certification
  CI workflow on a fresh native GitHub-hosted runner (never invented
  locally): per-target reproducibility for all four supported targets and
  the native Linux clean-machine lifecycle.
- **blocked_external_prerequisite**: hosted/external items with their exact
  prerequisite, never fabricated.
- **not_applicable**: documented unsupported targets (Windows).

## Results

Deterministic-local failures: $det_failures

See \`SUMMARY.json\` for the full row-by-row evidence and
\`secrets-scan.txt\` for the credential scan (0 findings required).
EOF

if [[ "$phase" == "full" ]]; then
  scan_output="$evidence_dir/secrets-scan.txt"
  {
    echo "secret-scan reproducible-packaging evidence $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "scan root: $evidence_dir"
    echo "scanned patterns: bearer authorization headers, GitHub token prefixes, Slack token prefixes, exported secret env assignments"
    echo "note: this file is self-excluded from the scan"
    echo "---"
    failures=0
    while IFS= read -r -d '' f; do
      rel="${f#"$evidence_dir"/}"
      if [[ "$rel" == "secrets-scan.txt" ]]; then
        echo "self-excluded: $rel"
        continue
      fi
      if grep -l -E "Authorization: Bearer|gho_[A-Za-z0-9]+|xox[baprs]-[A-Za-z0-9-]+|FUTUREDIFF_[A-Z_]+_TOKEN=[^[:space:]]+|dummy-token-never-used" "$f" >/dev/null 2>&1; then
        echo "LEAK: $rel"
        failures=$((failures + 1))
      else
        echo "clean: $rel"
      fi
    done < <(find "$evidence_dir" -type f -print0)
    echo "---"
    echo "leaks: $failures"
    if [[ $failures -ne 0 ]]; then exit 3; fi
  } > "$scan_output"
  scan_rc=$?
  if [[ $scan_rc -ne 0 ]]; then
    echo "error: secret scan found leaked credential material in evidence" >&2
    exit 3
  fi
  emit_det secret_scan_evidence deterministic_local credential_scan "no credential material in evidence" secrets-scan.txt True
fi

printf '%s\n' "${det_evidence[@]}" > "$cert_root/det.jsonl"
printf '%s\n' "${hosted_evidence[@]}" > "$cert_root/hosted.jsonl"
printf '%s\n' "${blocked_evidence[@]}" > "$cert_root/blocked.jsonl"
printf '%s\n' "${na_evidence[@]}" > "$cert_root/na.jsonl"

python3 - "$summary_file" "$evidence_dir" "$nonce" "$host" "$git_sha" "$det_failures" "$phase" \
  "$cert_root/det.jsonl" "$cert_root/hosted.jsonl" "$cert_root/blocked.jsonl" "$cert_root/na.jsonl" \
  "$target" "$sde" "$sde_iso" <<'PY'
import json, os, sys, time
summary_file, evidence_dir, nonce, host, git_sha = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
det_failures, phase = int(sys.argv[6]), sys.argv[7]
det = [json.loads(l) for l in open(sys.argv[8]) if l.strip()]
hosted = [json.loads(l) for l in open(sys.argv[9]) if l.strip()]
blocked = [json.loads(l) for l in open(sys.argv[10]) if l.strip()]
na = [json.loads(l) for l in open(sys.argv[11]) if l.strip()]
target, sde, sde_iso = sys.argv[12], int(sys.argv[13]), sys.argv[14]

def exists(observed_source):
    return not observed_source or observed_source == "<none>" or os.path.isfile(os.path.join(evidence_dir, observed_source))

problems = []
required_det = ["packaged_reproducible", "archive_metadata_normalized",
                "negative_wall_clock_independent", "negative_source_change_changes_digest",
                "negative_normalization_does_not_hide_payload", "negative_temp_dir_independent",
                "checksum_verify", "normalized_archive_sign_verify", "tamper_negative",
                "stale_checksum_negative", "sbom_valid", "provenance_bound", "release_verifier"]
det_by_id = {e["id"]: e for e in det}
for rid in required_det:
    if rid not in det_by_id:
        problems.append(f"missing deterministic_local row: {rid}")
    elif not exists(det_by_id[rid].get("observed_source", "")):
        problems.append(f"deterministic_local row {rid}: observed_source missing on disk")
if phase == "full":
    hosted_ids = {e["id"] for e in hosted}
    if "native_linux_clean_machine_lifecycle" not in hosted_ids:
        if not any(b["id"] == "native_linux_clean_machine_lifecycle" for b in blocked):
            problems.append("no native_linux_clean_machine_lifecycle hosted or blocked row")
    for e in hosted + blocked + na:
        if not exists(e.get("observed_source", "")):
            problems.append(f"row {e['id']}: observed_source missing on disk")
for e in det + hosted + blocked + na:
    if not exists(e.get("observed_source", "")):
        problems.append(f"row {e['id']}: observed_source missing on disk")

summary = {
    "kind": "reproducible-packaging-linux-lifecycle-certification",
    "generated_at": time.strftime("%Y%m%d-%H%M%S"),
    "host": host,
    "nonce": nonce,
    "git_sha": git_sha,
    "commit_tested": git_sha,
    "source_date_epoch": sde,
    "source_date_epoch_iso": sde_iso,
    "target": target,
    "confirmation_phrase_accepted": True,
    "deterministic_local": {"failures": det_failures, "evidence": det},
    "hosted_native_linux": {"evidence": hosted},
    "blocked_external_prerequisite": blocked,
    "not_applicable": na,
    "failures": det_failures,
    "assertion_problems": problems,
}
json.dump(summary, open(summary_file, "w"), indent=2)
if problems:
    print("SUMMARY assertion problems:", file=sys.stderr)
    for p in problems:
        print("  - " + p, file=sys.stderr)
    sys.exit(2)
print(json.dumps({"failures": summary["failures"], "deterministic_local": det_failures, "hosted": len(hosted), "blocked": len(blocked)}))
PY
summary_rc=$?
if [[ $summary_rc -ne 0 ]]; then
  echo "error: SUMMARY.json structural assertions failed (see stderr)" >&2
  exit 2
fi

if [[ "$phase" == "full" ]]; then
  if grep -q -E 'Authorization: Bearer|gho_[A-Za-z0-9]+|xox[baprs]-[A-Za-z0-9-]+|FUTUREDIFF_[A-Z_]+_TOKEN=[^[:space:]]+|dummy-token-never-used' "$summary_file"; then
    echo "LEAK: SUMMARY.json" >> "$scan_output"
    exit 3
  fi
  echo "clean: SUMMARY.json" >> "$scan_output"
fi

echo
echo "reproducible packaging certification evidence written to: $evidence_dir"
echo "deterministic_local failures: $det_failures"

if [[ $det_failures -ne 0 ]]; then
  echo "error: deterministic_local section has failures (failures=$det_failures); not a green run" >&2
  exit 1
fi
echo "OK: green run"
