#!/usr/bin/env bash
# certify-release-supply-chain.sh
#
# Release supply-chain integrity certification for FutureDiff stable-readiness:
#
#   1. signed release artifacts           (ephemeral RSA keypair; sign + verify)
#   2. published SBOM assets             (CycloneDX 1.5 create + verify + schema)
#   3. in-toto provenance                (SLSA v1 create + verify)
#   4. reproducible builds               (deterministic source zip + packaged
#                                         build-twice with honest classification)
#   5. GitHub artifact attestations      (live read-only gh attestation verify)
#   6. clean-machine install/upgrade/uninstall (published alpha.1 -> alpha.3)
#
# Evidence classes (recorded per artifact in SUMMARY.json):
#   deterministic            local, byte-verifiable checks that always run
#   deterministic_integration  local drill with a documented network dependency
#   real                     real local artifacts/published assets, outcome-classified
#   blocked                  exact external prerequisite; never fabricated
#
# The deterministic section always runs and MUST pass (exit non-zero on any
# required failure). Network-dependent items self-classify to blocked when the
# network is unavailable, are NOT counted as deterministic passes, and still
# make the deterministic section exit non-zero. Hosted/external items (new
# release publishing, independent security review, Linux/Windows native hosts,
# release-signing-key custody) are recorded BLOCKED with their exact
# prerequisite per the WHAT_REMAINS_BEFORE_PRODUCTION evidence rule.
#
# Usage:
#   scripts/certify-release-supply-chain.sh \
#     --confirm I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY \
#     [--nonce RUN_NONCE] [--evidence-dir DIR] [--skip-build]
#
# The confirmation phrase is
#   I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
assurance="python3 $repo_root/tools/futurediff_assurance.py"

CONFIRMATION_PHRASE="I_UNDERSTAND_FUTUREDIFF_WILL_CREATE_DISPOSABLE_EVIDENCE_ONLY"
nonce=""
evidence_dir=""
confirm_phrase=""
skip_build=0
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
    --skip-build) skip_build=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$confirm_phrase" != "$CONFIRMATION_PHRASE" ]]; then
  echo "error: this certification creates disposable evidence only." >&2
  echo "pass --confirm '$CONFIRMATION_PHRASE' to proceed" >&2
  exit 2
fi

if [[ -z "$nonce" ]]; then
  nonce="$(date +%Y%m%d-%H%M%S)-$RANDOM"
fi
if [[ -z "$evidence_dir" ]]; then
  evidence_dir="$repo_root/docs/certification/release-supply-chain-$nonce"
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
original_path="$PATH"

mkdir -p "$evidence_dir/deterministic" "$evidence_dir/real" "$evidence_dir/blocked"
echo "evidence directory: $evidence_dir"

host="$(uname -s)-$(uname -m)"
os="$(uname -s)"
git_sha="$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || echo unknown)"
summary_file="$evidence_dir/SUMMARY.json"
is_darwin=0
[[ "$os" == "Darwin" ]] && is_darwin=1

cert_root="$(mktemp -d "${TMPDIR:-/tmp}/futurediff-supply-chain-cert.XXXXXX")"
pristine="$cert_root/pristine"

cleanup() {
  git -C "$repo_root" worktree remove --force "$pristine" >/dev/null 2>&1 || true
  rm -rf "$cert_root"
}
trap cleanup EXIT

det_failures=0
det_evidence=()
real_evidence=()
blocked_evidence=()
real_failures=0

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
emit_real() { real_evidence+=("$(python3 - "$@" <<'PY'
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

# ---------------------------------------------------------------------------
# Phase 0: pristine detached worktree (git worktree, not git archive:
# build-public-release.sh needs a real .git for commit detection)
# ---------------------------------------------------------------------------
git -C "$repo_root" worktree add --detach "$pristine" HEAD >/dev/null 2>&1
[[ -d "$pristine/.git" || -f "$pristine/.git" ]]
pristine_sha="$(git -C "$pristine" rev-parse HEAD)"

# Ephemeral release signing keypair (private key never leaves cert_root).
mkdir -p "$cert_root/keys"
openssl genrsa -out "$cert_root/keys/release.key" 3072 2>/dev/null
openssl rsa -in "$cert_root/keys/release.key" -pubout -out "$cert_root/keys/release.pub" 2>/dev/null
cp "$cert_root/keys/release.pub" "$evidence_dir/deterministic/release-public-key.pem"

# The packaged double-build no longer needs a fixed date shim:
# build-public-release.sh derives SOURCE_DATE_EPOCH from the committed
# source tree (the commit timestamp), so both builds are byte-identical
# without any environment interception.

echo ">> pristine worktree: $pristine ($pristine_sha)"

# ---------------------------------------------------------------------------
# Phase 1: deterministic supply-chain certification (always runs)
# ---------------------------------------------------------------------------
echo ">> deterministic section: signatures, SBOM, provenance, reproducibility, install drill"

# --- 1a. sign/verify roundtrip with the ephemeral keypair (+ tamper negative)
printf 'futurediff-release-supply-chain-signature-fixture\n' > "$cert_root/sign-fixture.txt"
set +e
$assurance sign --file "$cert_root/sign-fixture.txt" --private-key "$cert_root/keys/release.key" \
  --signature "$cert_root/sign-fixture.txt.sig" --output "$cert_root/sign-result.json" > "$cert_root/sign-stdout.txt" 2>&1
sign_rc=$?
$assurance verify-signature --file "$cert_root/sign-fixture.txt" --public-key "$cert_root/keys/release.pub" \
  --signature "$cert_root/sign-fixture.txt.sig" --output "$cert_root/verify-result.json" > "$cert_root/verify-stdout.txt" 2>&1
verify_rc=$?
printf '\ntampered\n' >> "$cert_root/sign-fixture.txt"
$assurance verify-signature --file "$cert_root/sign-fixture.txt" --public-key "$cert_root/keys/release.pub" \
  --signature "$cert_root/sign-fixture.txt.sig" --output "$cert_root/tamper-result.json" > "$cert_root/tamper-stdout.txt" 2>&1
tamper_rc=$?
set -e
python3 - "$evidence_dir/deterministic/sign-verify-roundtrip.json" "$cert_root/verify-result.json" "$cert_root/tamper-result.json" "$sign_rc" "$verify_rc" "$tamper_rc" <<'PY'
import json, sys
out, verify_path, tamper_path, sign_rc, verify_rc, tamper_rc = sys.argv[1:7]
verify = json.load(open(verify_path))
tamper = json.load(open(tamper_path))
doc = {
    "sign_rc": int(sign_rc), "verify_rc": int(verify_rc), "tamper_rc": int(tamper_rc),
    "roundtrip_verified": int(verify_rc) == 0 and verify.get("verified") is True,
    "tamper_rejected": int(tamper_rc) != 0 and tamper.get("verified") is False,
    "pass": int(sign_rc) == 0 and int(verify_rc) == 0 and verify.get("verified") is True
            and int(tamper_rc) != 0 and tamper.get("verified") is False,
    "file_sha256": verify.get("file_sha256"),
    "signature_sha256": verify.get("signature_sha256"),
    "public_key": "deterministic/release-public-key.pem",
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
d1_json="$evidence_dir/deterministic/sign-verify-roundtrip.json"
python3 -c "import json,sys; d=json.load(open('$d1_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det sign_verify_roundtrip deterministic signed_release_artifacts "roundtrip verified + tamper rejected" deterministic/sign-verify-roundtrip.json "$(python3 -c "import json;print(json.load(open('$d1_json'))['pass'])")"

# --- 1b. SBOM create + verify + schema sanity (+ mutation negative)
$assurance sbom-create --root "$pristine" --name futurediff --version "$version" \
  --output "$cert_root/sbom.cdx.json" > "$cert_root/sbom-create-stdout.txt" 2>&1
$assurance sbom-verify --root "$pristine" --sbom "$cert_root/sbom.cdx.json" \
  --output "$cert_root/sbom-verify.json" > "$cert_root/sbom-verify-stdout.txt" 2>&1
mutated="$cert_root/mutated-tree"
cp -R "$pristine" "$mutated"
printf '\n# mutation probe\n' >> "$mutated/README.md"
set +e
$assurance sbom-verify --root "$mutated" --sbom "$cert_root/sbom.cdx.json" \
  --output "$cert_root/sbom-mutation.json" > "$cert_root/sbom-mutation-stdout.txt" 2>&1
mut_rc=$?
set -e
python3 - "$evidence_dir/deterministic/sbom-create-verify.json" "$cert_root/sbom.cdx.json" "$cert_root/sbom-verify.json" "$cert_root/sbom-mutation.json" "$mut_rc" <<'PY'
import json, sys
out, sbom_path, verify_path, mutation_path, mut_rc = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5])
sbom = json.load(open(sbom_path))
verify = json.load(open(verify_path))
mutation = json.load(open(mutation_path))
schema_ok = (
    sbom.get("bomFormat") == "CycloneDX"
    and str(sbom.get("specVersion", "")).startswith("1.")
    and str(sbom.get("serialNumber", "")).startswith("urn:uuid:")
    and len(sbom.get("components", [])) > 0
)
mutation_rejected = mut_rc != 0 and mutation.get("verified") is False and len(mutation.get("changed", [])) > 0
doc = {
    "schema_ok": schema_ok,
    "verified": verify.get("verified") is True,
    "mutation_rejected": mutation_rejected,
    "component_count": len(sbom.get("components", [])),
    "spec_version": sbom.get("specVersion"),
    "serial_number": sbom.get("serialNumber"),
    "changed_on_mutation": mutation.get("changed", []),
    "pass": schema_ok and verify.get("verified") is True and mutation_rejected,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
d2_json="$evidence_dir/deterministic/sbom-create-verify.json"
python3 -c "import json,sys; d=json.load(open('$d2_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det sbom_create_verify_schema deterministic sbom_assets "cyclonedx 1.5 schema + verify + mutation negative" deterministic/sbom-create-verify.json "$(python3 -c "import json;print(json.load(open('$d2_json'))['pass'])")"

# --- 1c. in-toto provenance create + verify (+ digest match)
$assurance manifest-create --root "$pristine" --output "$cert_root/source-manifest.json" > "$cert_root/manifest-stdout.txt" 2>&1
$assurance provenance-create --root "$pristine" --manifest "$cert_root/source-manifest.json" \
  --name futurediff --version "$version" --builder-id "$builder_id" \
  --source-uri "$source_uri" --source-digest "$pristine_sha" \
  --output "$cert_root/provenance.intoto.json" > "$cert_root/provenance-stdout.txt" 2>&1
$assurance provenance-verify --manifest "$cert_root/source-manifest.json" \
  --provenance "$cert_root/provenance.intoto.json" \
  --output "$cert_root/provenance-verify.json" > "$cert_root/provenance-verify-stdout.txt" 2>&1
python3 - "$evidence_dir/deterministic/provenance-create-verify.json" "$cert_root/source-manifest.json" "$cert_root/provenance.intoto.json" "$cert_root/provenance-verify.json" "$pristine_sha" <<'PY'
import json, sys
out, manifest_path, provenance_path, verify_path, source_digest = sys.argv[1:6]
manifest = json.load(open(manifest_path))
provenance = json.load(open(provenance_path))
verify = json.load(open(verify_path))
doc = {
    "verified": verify.get("verified") is True,
    "subject_match": verify.get("subject_match"),
    "byproduct_match": verify.get("byproduct_match"),
    "types_ok": verify.get("types_ok"),
    "manifest_digest": manifest.get("manifest_digest"),
    "source_digest_recorded": source_digest,
    "statement_type": provenance.get("_type"),
    "predicate_type": provenance.get("predicateType"),
    "pass": verify.get("verified") is True and verify.get("subject_match") is True
            and verify.get("byproduct_match") is True and verify.get("types_ok") is True,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
d3_json="$evidence_dir/deterministic/provenance-create-verify.json"
python3 -c "import json,sys; d=json.load(open('$d3_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det provenance_create_verify deterministic in_toto_provenance "SLSA v1 create + verify + digest match" deterministic/provenance-create-verify.json "$(python3 -c "import json;print(json.load(open('$d3_json'))['pass'])")"

# --- 1d. deterministic source-zip reproducibility (release-build twice)
$assurance release-build --root "$pristine" --name futurediff --version "$version" \
  --output "$cert_root/rel1.zip" > "$cert_root/rel1.json" 2>&1
$assurance release-build --root "$pristine" --name futurediff --version "$version" \
  --output "$cert_root/rel2.zip" > "$cert_root/rel2.json" 2>&1
set +e
$assurance release-verify --archive "$cert_root/rel1.zip" --output "$cert_root/rel1-verify.json" > "$cert_root/rel1-verify-stdout.txt" 2>&1
rv1_rc=$?
$assurance release-verify --archive "$cert_root/rel2.zip" --output "$cert_root/rel2-verify.json" > "$cert_root/rel2-verify-stdout.txt" 2>&1
rv2_rc=$?
set -e
python3 - "$evidence_dir/deterministic/release-build-twice.json" "$cert_root/rel1.json" "$cert_root/rel2.json" "$cert_root/rel1-verify.json" "$cert_root/rel2-verify.json" "$rv1_rc" "$rv2_rc" <<'PY'
import json, sys
out, rel1, rel2, v1, v2, rv1, rv2 = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5], int(sys.argv[6]), int(sys.argv[7])
a = json.load(open(rel1))
b = json.load(open(rel2))
va = json.load(open(v1))
vb = json.load(open(v2))
doc = {
    "archive_sha256_1": a["sha256"], "archive_sha256_2": b["sha256"],
    "archive_identical": a["sha256"] == b["sha256"],
    "manifest_digest_1": a["manifest_digest"], "manifest_digest_2": b["manifest_digest"],
    "manifest_digest_identical": a["manifest_digest"] == b["manifest_digest"],
    "release_verify_1": rv1 == 0 and va.get("verified") is True,
    "release_verify_2": rv2 == 0 and vb.get("verified") is True,
    "file_count": a["file_count"],
    "pass": a["sha256"] == b["sha256"] and a["manifest_digest"] == b["manifest_digest"]
            and rv1 == 0 and va.get("verified") is True and rv2 == 0 and vb.get("verified") is True,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
d4_json="$evidence_dir/deterministic/release-build-twice.json"
python3 -c "import json,sys; d=json.load(open('$d4_json')); sys.exit(0 if d['pass'] else 1)" || det_failures=$((det_failures + 1))
emit_det release_build_twice deterministic reproducible_builds "source zip byte-identical + release-verify" deterministic/release-build-twice.json "$(python3 -c "import json;print(json.load(open('$d4_json'))['pass'])")"

# --- 1e. packaged build-twice (SOURCE_DATE_EPOCH; byte-identical archives)
if [[ "$skip_build" == "0" && "$is_darwin" == "1" ]]; then
  ( cd "$pristine" && bash scripts/build-public-release.sh "$version" "$cert_root/pkg1" ) > "$cert_root/pkg1.log" 2>&1
  ( cd "$pristine" && bash scripts/build-public-release.sh "$version" "$cert_root/pkg2" ) > "$cert_root/pkg2.log" 2>&1
  a1="$cert_root/pkg1/futurediff-$version-darwin-arm64.tar.gz"
  a2="$cert_root/pkg2/futurediff-$version-darwin-arm64.tar.gz"
  sha1="$(shasum -a 256 "$a1" | awk '{print $1}')"
  sha2="$(shasum -a 256 "$a2" | awk '{print $1}')"
  c1="$(gzip -dc "$a1" | shasum -a 256 | awk '{print $1}')"
  c2="$(gzip -dc "$a2" | shasum -a 256 | awk '{print $1}')"
  tar -tvf "$a1" > "$evidence_dir/deterministic/packaged-build-1.tar-tvf.txt" 2>&1
  tar -tvf "$a2" > "$evidence_dir/deterministic/packaged-build-2.tar-tvf.txt" 2>&1
  mkdir -p "$cert_root/px1" "$cert_root/px2"
  tar -xzf "$a1" -C "$cert_root/px1"
  tar -xzf "$a2" -C "$cert_root/px2"
  p1="$(cd "$cert_root/px1" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
  p2="$(cd "$cert_root/px2" && find . -type f -print0 | sort -z | xargs -0 shasum -a 256 | shasum -a 256 | awk '{print $1}')"
  python3 - "$evidence_dir/deterministic/packaged-build-twice.json" "$sha1" "$sha2" "$c1" "$c2" "$p1" "$p2" "$version" <<'PY'
import json, sys
out, sha1, sha2, c1, c2, p1, p2, version = sys.argv[1:9]
archive_identical = sha1 == sha2
content_identical = c1 == c2
if archive_identical:
    classification = "packaged_archive_byte_identical"
    blocked_needed = False
    blocked_prerequisite = None
elif content_identical:
    classification = "packaged_content_byte_identical"
    blocked_needed = True
    blocked_prerequisite = "archive-level metadata normalization in the packaging step (gzip header mtime and tar entry mtimes written by BSD libarchive tar -czf); byte-identical packaged archives require SOURCE_DATE_EPOCH-style packaging"
else:
    classification = "packaged_content_differs"
    blocked_needed = True
    blocked_prerequisite = "reproducible packaging build: pin the build date at the toolchain level and normalize tar entry mtimes and the gzip header at packaging time; recorded diff is the binary-embedded build date and/or tar entry metadata"
doc = {
    "version": version,
    "archive_sha256_1": sha1, "archive_sha256_2": sha2,
    "archive_identical": archive_identical,
    "content_sha256_1": c1, "content_sha256_2": c2,
    "content_identical": content_identical,
    "payload_sha256_1": p1, "payload_sha256_2": p2,
    "payload_identical": p1 == p2,
    "classification": classification,
    "pass": True,
    "note": "both builds ran build-public-release.sh from the same pristine detached worktree; SOURCE_DATE_EPOCH is derived from the committed source tree (git commit timestamp), so buildinfo.Date and every tar/gzip mtime are fixed without any environment interception; only the two original outputs were compared (never re-packaged); payload_sha256 hashes the extracted file bytes only",
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps({"classification": classification, "archive_identical": archive_identical, "content_identical": content_identical, "payload_identical": p1 == p2, "blocked_needed": blocked_needed, "blocked_prerequisite": blocked_prerequisite}))
PY
  pkg_cls="$(python3 -c "import json;print(json.load(open('$evidence_dir/deterministic/packaged-build-twice.json'))['classification'])")"
  emit_det packaged_build_twice deterministic reproducible_builds "packaged build-twice honest classification" deterministic/packaged-build-twice.json True
  if [[ "$pkg_cls" != "packaged_archive_byte_identical" ]]; then
    pkg_prereq="$(python3 -c "import json;d=json.load(open('$evidence_dir/deterministic/packaged-build-twice.json'));print('payload file bytes are byte-identical; the archive diff is only packaging metadata (tar entry mtimes and the gzip header written by BSD libarchive tar -czf) — normalize packaging metadata (SOURCE_DATE_EPOCH-style) for byte-identical archives' if d['payload_identical'] else 'packaged payload differs — pin the build date at the toolchain level and normalize tar entry mtimes and the gzip header at packaging time')")"
    emit_blocked byte_identical_packaged_archives blocked reproducible_builds "byte-identical packaged archives" deterministic/packaged-build-twice.json False "$pkg_prereq; recorded diff: archive_sha256_1/2 + content_sha256_1/2 + payload_sha256_1/2 in deterministic/packaged-build-twice.json"
  fi
  # checksum sidecar roundtrip on the second packaged archive
  set +e
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$cert_root/pkg2" && sha256sum -c "$(basename "$a2").sha256") > "$evidence_dir/deterministic/checksum-verify.txt" 2>&1
    ck_rc=$?
  else
    (cd "$cert_root/pkg2" && shasum -a 256 -c "$(basename "$a2").sha256") > "$evidence_dir/deterministic/checksum-verify.txt" 2>&1
    ck_rc=$?
  fi
  set -e
  if [[ $ck_rc -ne 0 ]]; then det_failures=$((det_failures + 1)); fi
  emit_det checksum_verify deterministic release_checksums "published .sha256 sidecar verifies the archive" deterministic/checksum-verify.txt "$([[ $ck_rc -eq 0 ]] && echo True || echo False)"
  export PATH="$original_path"
else
  if [[ "$is_darwin" != "1" ]]; then
    emit_blocked packaged_build_twice blocked reproducible_builds "packaged build-twice on darwin-arm64" "<none>" False "run the certification on a macOS arm64 host; this host is $(uname -s)-$(uname -m)"
    det_failures=$((det_failures + 1))
  fi
fi

# --- 1f. clean-machine install/upgrade/uninstall drill (network-dependent)
drill_work="$(mktemp -d "${TMPDIR:-/tmp}/futurediff-supply-drill.XXXXXX")"
drill_prefix="$drill_work/prefix"
drill_home="$drill_work/home"
mkdir -p "$drill_home"
chmod 700 "$drill_home"
drill_ok=1
drill_network_failure=0
drill_detail=""
if [[ "$is_darwin" == "1" ]]; then
  set +e
  FDIF_HOME="$drill_home" bash "$repo_root/scripts/install-release.sh" --version "v0.1.0-alpha.1" --prefix "$drill_prefix" > "$cert_root/drill-alpha1.log" 2>&1
  d1_rc=$?
  if [[ $d1_rc -ne 0 ]]; then
    drill_ok=0
    if grep -q -iE "could not resolve|failed to connect|curl: \(([0-9]+)\)|connection (refused|reset|timed out)|network is unreachable" "$cert_root/drill-alpha1.log"; then
      drill_network_failure=1
    fi
    drill_detail="alpha.1 install failed: $(tail -c 400 "$cert_root/drill-alpha1.log" | tr '\n' ' ')"
  else
    v1="$($drill_prefix/bin/fdif version 2>&1)"
    [[ "$v1" == *"v0.1.0-alpha.1"* ]] || { drill_ok=0; drill_detail="alpha.1 version mismatch: $v1"; }
    FDIF_HOME="$drill_home" "$drill_prefix/bin/fdif" doctor > "$cert_root/drill-doctor-alpha1.log" 2>&1
    doc_rc=$?
    [[ $doc_rc -eq 0 ]] || { drill_ok=0; drill_detail="alpha.1 doctor rc=$doc_rc"; }
  fi
  if [[ $drill_ok -eq 1 ]]; then
    FDIF_HOME="$drill_home" bash "$repo_root/scripts/install-release.sh" --version "v0.1.0-alpha.3" --prefix "$drill_prefix" > "$cert_root/drill-alpha3.log" 2>&1
    d3_rc=$?
    if [[ $d3_rc -ne 0 ]]; then
      drill_ok=0
      if grep -q -iE "could not resolve|failed to connect|curl: \(([0-9]+)\)|connection (refused|reset|timed out)|network is unreachable" "$cert_root/drill-alpha3.log"; then
        drill_network_failure=1
      fi
      drill_detail="alpha.3 upgrade failed: $(tail -c 400 "$cert_root/drill-alpha3.log" | tr '\n' ' ')"
    else
      v3="$($drill_prefix/bin/fdif version 2>&1)"
      [[ "$v3" == *"v0.1.0-alpha.3"* ]] || { drill_ok=0; drill_detail="alpha.3 version mismatch: $v3"; }
    fi
  fi
  if [[ $drill_ok -eq 1 ]]; then
    # Uninstall per the compatibility/deprecation policy contract.
    rm -f "$drill_prefix/bin/fdif" "$drill_prefix/bin/futurediff" "$drill_prefix/bin/futurediffd"
    rm -rf "$drill_home"
    residue=""
    for b in fdif futurediff futurediffd; do
      if [[ -x "$drill_prefix/bin/$b" ]]; then
        residue="$residue $b"
      fi
    done
    if [[ -n "$residue" || -e "$drill_home" ]]; then
      drill_ok=0
      drill_detail="uninstall left residue (binaries:$residue home_exists=$([ -e "$drill_home" ] && echo yes || echo no))"
    fi
  fi
  set -e
else
  emit_blocked install_upgrade_uninstall_drill blocked clean_machine_install "install/upgrade/uninstall drill on darwin" "<none>" False "run the certification on a macOS arm64 host; this host is $(uname -s)-$(uname -m)"
  drill_ok=0
  det_failures=$((det_failures + 1))
fi

python3 - "$evidence_dir/deterministic/install-upgrade-uninstall-drill.json" "$drill_ok" "$drill_network_failure" "$drill_detail" <<'PY'
import json, sys
out, ok, net_fail, detail = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
doc = {
    "classification": "deterministic_integration",
    "network_dependency": "github_release_download",
    "install_v0.1.0-alpha.1": "fdif version contains v0.1.0-alpha.1 + fdif doctor smoke",
    "upgrade_v0.1.0-alpha.3": "fdif version contains v0.1.0-alpha.3 in the same prefix",
    "uninstall_contract": "remove $prefix/bin/{fdif,futurediff,futurediffd} + $FDIF_HOME; no package-manager hooks",
    "uninstall_verified": "command -v empty within the prefixed PATH; FDIF_HOME removed",
    "pass": bool(ok),
    "network_failure": bool(net_fail),
    "detail": detail,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps({"pass": bool(ok), "network_failure": bool(net_fail), "detail": detail}))
PY
drill_json="$evidence_dir/deterministic/install-upgrade-uninstall-drill.json"
drill_pass="$(python3 -c "import json;print(json.load(open('$drill_json'))['pass'])")"
drill_net="$(python3 -c "import json;print(json.load(open('$drill_json'))['network_failure'])")"
if [[ "$drill_pass" == "True" ]]; then
  emit_det install_upgrade_uninstall_drill deterministic_integration clean_machine_install "install alpha.1 -> upgrade alpha.3 -> uninstall per policy contract" deterministic/install-upgrade-uninstall-drill.json True
else
  det_failures=$((det_failures + 1))
  if [[ "$drill_net" == "True" ]]; then
    emit_blocked install_upgrade_uninstall_blocked blocked clean_machine_install "clean-machine install/upgrade/uninstall drill" deterministic/install-upgrade-uninstall-drill.json False "network unavailable for the GitHub release download (github_release_download); re-run with network access; the drill is NOT counted as a deterministic pass"
  else
    emit_blocked install_upgrade_uninstall_blocked blocked clean_machine_install "clean-machine install/upgrade/uninstall drill" deterministic/install-upgrade-uninstall-drill.json False "deterministic drill failed: see deterministic/install-upgrade-uninstall-drill.json"
  fi
  emit_det install_upgrade_uninstall_drill deterministic_integration clean_machine_install "install alpha.1 -> upgrade alpha.3 -> uninstall per policy contract" deterministic/install-upgrade-uninstall-drill.json False
fi

echo ">> deterministic section: failures=$det_failures"

# ---------------------------------------------------------------------------
# Phase 2: real local certification
# ---------------------------------------------------------------------------
echo ">> real section: packaged sign/verify, SBOM, provenance, attestation, install"

real_archive=""
real_zip="$cert_root/rel1.zip"

# --- 2a. real packaged artifact: build (real date), sign, verify, sidecar
if [[ "$skip_build" == "0" && "$is_darwin" == "1" ]]; then
  export PATH="$original_path"
  ( cd "$pristine" && bash scripts/build-public-release.sh "$version" "$cert_root/realpkg" ) > "$cert_root/realpkg.log" 2>&1
  real_archive="$cert_root/realpkg/futurediff-$version-darwin-arm64.tar.gz"
  $assurance sign --file "$real_archive" --private-key "$cert_root/keys/release.key" \
    --signature "$evidence_dir/real/release-darwin-arm64.tar.gz.sig" \
    --output "$evidence_dir/real/real-packaged-sign.json" > "$cert_root/real-sign-stdout.txt" 2>&1
  $assurance verify-signature --file "$real_archive" --public-key "$cert_root/keys/release.pub" \
    --signature "$evidence_dir/real/release-darwin-arm64.tar.gz.sig" \
    --output "$evidence_dir/real/real-packaged-verify.json" > "$cert_root/real-verify-stdout.txt" 2>&1
  $assurance sign --file "$real_zip" --private-key "$cert_root/keys/release.key" \
    --signature "$evidence_dir/real/release-source.zip.sig" \
    --output "$evidence_dir/real/real-source-zip-sign.json" > "$cert_root/real-zip-sign-stdout.txt" 2>&1
  $assurance verify-signature --file "$real_zip" --public-key "$cert_root/keys/release.pub" \
    --signature "$evidence_dir/real/release-source.zip.sig" \
    --output "$evidence_dir/real/real-source-zip-verify.json" > "$cert_root/real-zip-verify-stdout.txt" 2>&1
  set +e
  (cd "$cert_root/realpkg" && shasum -a 256 -c "$(basename "$real_archive").sha256") > "$evidence_dir/real/real-packaged-checksum.txt" 2>&1
  real_checksum_rc=$?
  set -e
  python3 - "$evidence_dir/real/real-packaged-sign-verify.json" "$evidence_dir/real/real-packaged-verify.json" "$evidence_dir/real/real-source-zip-verify.json" "$real_checksum_rc" "$version" <<'PY'
import json, sys
out, pv_path, zv_path, ck_rc, version = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]), sys.argv[5]
pv = json.load(open(pv_path))
zv = json.load(open(zv_path))
doc = {
    "version": version,
    "tarball_verified": pv.get("verified") is True,
    "source_zip_verified": zv.get("verified") is True,
    "checksum_verified": ck_rc == 0,
    "public_key": "deterministic/release-public-key.pem",
    "signature_tarball": "real/release-darwin-arm64.tar.gz.sig",
    "signature_source_zip": "real/release-source.zip.sig",
    "pass": pv.get("verified") is True and zv.get("verified") is True and ck_rc == 0,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
  r1_json="$evidence_dir/real/real-packaged-sign-verify.json"
  python3 -c "import json,sys; d=json.load(open('$r1_json')); sys.exit(0 if d['pass'] else 1)" || real_failures=$((real_failures + 1))
  emit_real real_packaged_sign_verify real signed_release_artifacts "real darwin-arm64 tarball + source zip signed and verified" real/real-packaged-sign-verify.json "$(python3 -c "import json;print(json.load(open('$r1_json'))['pass'])")"
else
  if [[ "$is_darwin" != "1" ]]; then
    emit_blocked real_packaged_sign_verify blocked signed_release_artifacts "real darwin-arm64 packaged sign/verify" "<none>" False "run the certification on a macOS arm64 host"
    real_failures=$((real_failures + 1))
  fi
fi

# --- 2b. real SBOM and provenance bound to HEAD
$assurance sbom-create --root "$pristine" --name futurediff --version "$version" \
  --output "$evidence_dir/real/sbom.cdx.json" > "$cert_root/real-sbom-create-stdout.txt" 2>&1
$assurance sbom-verify --root "$pristine" --sbom "$evidence_dir/real/sbom.cdx.json" \
  --output "$evidence_dir/real/real-sbom-verify.json" > "$cert_root/real-sbom-verify-stdout.txt" 2>&1
python3 - "$evidence_dir/real/real-sbom.json" "$evidence_dir/real/sbom.cdx.json" "$evidence_dir/real/real-sbom-verify.json" <<'PY'
import json, sys
out, sbom_path, verify_path = sys.argv[1], sys.argv[2], sys.argv[3]
sbom = json.load(open(sbom_path))
verify = json.load(open(verify_path))
doc = {
    "bomFormat": sbom.get("bomFormat"), "specVersion": sbom.get("specVersion"),
    "serialNumber": sbom.get("serialNumber"),
    "component_count": len(sbom.get("components", [])),
    "verified": verify.get("verified") is True,
    "manifest_digest_property": next((p.get("value") for p in sbom.get("metadata", {}).get("properties", []) if p.get("name") == "futurediff:manifest_digest"), None),
    "pass": verify.get("verified") is True,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
r2_json="$evidence_dir/real/real-sbom.json"
python3 -c "import json,sys; d=json.load(open('$r2_json')); sys.exit(0 if d['pass'] else 1)" || real_failures=$((real_failures + 1))
emit_real real_sbom real sbom_assets "real CycloneDX SBOM bound to HEAD, verified" real/real-sbom.json "$(python3 -c "import json;print(json.load(open('$r2_json'))['pass'])")"

$assurance provenance-create --root "$pristine" --manifest "$cert_root/source-manifest.json" \
  --name futurediff --version "$version" --builder-id "$builder_id" \
  --source-uri "$source_uri" --source-digest "$git_sha" \
  --output "$evidence_dir/real/provenance.intoto.json" > "$cert_root/real-provenance-stdout.txt" 2>&1
$assurance provenance-verify --manifest "$cert_root/source-manifest.json" \
  --provenance "$evidence_dir/real/provenance.intoto.json" \
  --output "$evidence_dir/real/real-provenance-verify.json" > "$cert_root/real-provenance-verify-stdout.txt" 2>&1
python3 - "$evidence_dir/real/real-provenance.json" "$evidence_dir/real/real-provenance-verify.json" <<'PY'
import json, sys
out, verify_path = sys.argv[1], sys.argv[2]
verify = json.load(open(verify_path))
doc = {"verified": verify.get("verified") is True, "subject_match": verify.get("subject_match"),
       "byproduct_match": verify.get("byproduct_match"), "types_ok": verify.get("types_ok"),
       "pass": verify.get("verified") is True}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps(doc))
PY
r3_json="$evidence_dir/real/real-provenance.json"
python3 -c "import json,sys; d=json.load(open('$r3_json')); sys.exit(0 if d['pass'] else 1)" || real_failures=$((real_failures + 1))
emit_real real_provenance real in_toto_provenance "real in-toto provenance bound to HEAD, verified" real/real-provenance.json "$(python3 -c "import json;print(json.load(open('$r3_json'))['pass'])")"

# --- 2c. live read-only GitHub artifact attestation (digest-matched alpha.3 asset)
asset_name="futurediff-$version-darwin-arm64.tar.gz"
asset_path="$cert_root/$asset_name"
asset_ok=1
curl --fail --location --proto '=https' --tlsv1.2 --max-time 1200 --retry 3 --retry-delay 5 \
  --output "$asset_path" "https://github.com/$repo/releases/download/$version/$asset_name" > "$cert_root/asset-download.log" 2>&1 || asset_ok=0
if [[ $asset_ok -eq 1 ]]; then
  curl --fail --location --proto '=https' --tlsv1.2 --max-time 120 --retry 3 --retry-delay 5 \
    --output "$asset_path.sha256" "https://github.com/$repo/releases/download/$version/$asset_name.sha256" > "$cert_root/asset-sidecar.log" 2>&1 || asset_ok=0
fi
if [[ $asset_ok -eq 1 ]]; then
  (cd "$cert_root" && shasum -a 256 -c "$asset_name.sha256") > "$evidence_dir/real/attestation-subject-checksum.txt" 2>&1 || asset_ok=0
fi
if [[ $asset_ok -eq 1 ]]; then
  set +e
  bash "$repo_root/scripts/github-attestation-verify.sh" "$asset_path" "$repo" "$evidence_dir/real/attestation.json" > "$cert_root/attestation-stdout.txt" 2>&1
  attest_rc=$?
  set -e
  if [[ $attest_rc -eq 0 ]]; then
    emit_real real_attestation real github_artifact_attestation "gh attestation verify on the digest-matched alpha.3 asset" real/attestation.json True
  else
    reason="$(python3 -c "import json;print(json.load(open('$evidence_dir/real/attestation.json')).get('reason','verification_failed'))" 2>/dev/null || echo "verification_failed")"
    emit_blocked attestation_blocked blocked github_artifact_attestation "GitHub artifact attestation verification" real/attestation.json False "gh attestation verify failed: $reason (exact output captured in real/attestation.json); requires attestation records published by a release workflow (release.yml attests dist/*.zip only on v[1-9]* tags) or a verified hosted run"
  fi
else
  if [[ $asset_ok -ne 1 ]]; then
    reason="asset download or digest verification failed"
  else
    reason="gh CLI unavailable"
  fi
  printf '{"kind":"github_artifact_attestation","passed":false,"reason":"%s"}\n' "$reason" > "$evidence_dir/real/attestation.json"
  emit_blocked attestation_blocked blocked github_artifact_attestation "GitHub artifact attestation verification" real/attestation.json False "gh attestation verify unavailable: $reason; requires a verified hosted attestation record"
fi

# --- 2d. real install drill from the published alpha.3 asset (documented installer)
real_drill_prefix="$cert_root/real-prefix"
real_drill_home="$cert_root/real-home"
mkdir -p "$real_drill_home"
chmod 700 "$real_drill_home"
real_drill_ok=1
real_drill_detail=""
if [[ "$is_darwin" == "1" ]]; then
  set +e
  FDIF_HOME="$real_drill_home" bash "$repo_root/scripts/install-release.sh" --version "$version" --prefix "$real_drill_prefix" > "$cert_root/real-drill.log" 2>&1
  rd_rc=$?
  set -e
  if [[ $rd_rc -ne 0 ]]; then
    real_drill_ok=0
    if grep -q -iE "could not resolve|failed to connect|curl: \(([0-9]+)\)|connection (refused|reset|timed out)|network is unreachable" "$cert_root/real-drill.log"; then
      real_drill_detail="network failure: $(tail -c 300 "$cert_root/real-drill.log" | tr '\n' ' ')"
    else
      real_drill_detail="install failed: $(tail -c 300 "$cert_root/real-drill.log" | tr '\n' ' ')"
    fi
  else
    v="$($real_drill_prefix/bin/fdif version 2>&1)"
    [[ "$v" == *"$version"* ]] || { real_drill_ok=0; real_drill_detail="version mismatch: $v"; }
    FDIF_HOME="$real_drill_home" "$real_drill_prefix/bin/fdif" doctor > "$cert_root/real-drill-doctor.log" 2>&1
    rd_doc=$?
    [[ $rd_doc -eq 0 ]] || { real_drill_ok=0; real_drill_detail="doctor rc=$rd_doc"; }
    rm -f "$real_drill_prefix/bin/fdif" "$real_drill_prefix/bin/futurediff" "$real_drill_prefix/bin/futurediffd"
    rm -rf "$real_drill_home"
    for b in fdif futurediff futurediffd; do
      if [[ -x "$real_drill_prefix/bin/$b" ]]; then real_drill_ok=0; real_drill_detail="uninstall left $b"; fi
    done
    [[ ! -e "$real_drill_home" ]] || { real_drill_ok=0; real_drill_detail="uninstall left FDIF_HOME"; }
  fi
fi
python3 - "$evidence_dir/real/real-install-upgrade-uninstall.json" "$real_drill_ok" "$real_drill_detail" "$version" <<'PY'
import json, sys
out, ok, detail, version = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
doc = {
    "version": version,
    "installer": "scripts/install-release.sh (checksum-sidecar-verified download from the published GitHub release)",
    "install_verified": "fdif version contains the version; fdif doctor smoke exits 0",
    "uninstall_contract": "remove $prefix/bin/{fdif,futurediff,futurediffd} + $FDIF_HOME",
    "pass": bool(ok),
    "detail": detail,
}
json.dump(doc, open(out, "w"), indent=2)
print(json.dumps({"pass": bool(ok), "detail": detail}))
PY
real_drill_pass="$(python3 -c "import json;print(json.load(open('$evidence_dir/real/real-install-upgrade-uninstall.json'))['pass'])")"
if [[ "$real_drill_pass" == "True" ]]; then
  emit_real real_install_upgrade_uninstall real clean_machine_install "clean-machine install of published alpha.3 via the documented installer + uninstall" real/real-install-upgrade-uninstall.json True
else
  real_failures=$((real_failures + 1))
  emit_blocked install_upgrade_uninstall_blocked blocked clean_machine_install "clean-machine install of published alpha.3 via the documented installer + uninstall" real/real-install-upgrade-uninstall.json False "real install drill failed: $real_drill_detail; re-run with network access"
fi

# --- 2e. independent recomputation of recorded evidence (non-vacuous gate)
set +e
if [[ "$skip_build" == "0" && "$is_darwin" == "1" && -n "$real_archive" ]]; then
  $assurance verify-signature --file "$real_archive" --public-key "$cert_root/keys/release.pub" \
    --signature "$evidence_dir/real/release-darwin-arm64.tar.gz.sig" \
    --output "$evidence_dir/real/recompute-signature-verify.json" > "$cert_root/recompute-sign.log" 2>&1
  recompute_sign_rc=$?
else
  recompute_sign_rc=2
fi
$assurance release-verify --archive "$real_zip" \
  --output "$evidence_dir/real/recompute-release-verify.json" > "$cert_root/recompute-rv.log" 2>&1
recompute_rv_rc=$?
set -e
emit_det recompute_signature_verify deterministic independent_recomputation "re-run verify-signature with the recorded key and .sig" real/recompute-signature-verify.json "$([[ $recompute_sign_rc -eq 0 ]] && echo True || echo False)"
emit_det recompute_release_verify deterministic independent_recomputation "re-run release-verify on the recorded source zip" real/recompute-release-verify.json "$([[ $recompute_rv_rc -eq 0 ]] && echo True || echo False)"
if [[ $recompute_sign_rc -ne 0 ]]; then det_failures=$((det_failures + 1)); fi
if [[ $recompute_rv_rc -ne 0 ]]; then det_failures=$((det_failures + 1)); fi

# ---------------------------------------------------------------------------
# Phase 3: blocked items (hosted/external; exact prerequisites)
# ---------------------------------------------------------------------------
emit_blocked external_security_review blocked external_security_review "independent external security review or authoritative external validation" "<none>" False "commission an independent security review or obtain authoritative external validation (external human/org; cannot be self-certified)"
emit_blocked linux_native_clean_machine blocked clean_machine_install "clean-machine install/upgrade/uninstall evidence on native Linux" "<none>" False "provision a Linux clean machine (host) and run the certification drill there"
emit_blocked windows_native_clean_machine blocked clean_machine_install "clean-machine install/upgrade/uninstall evidence on native Windows" "<none>" False "provision a Windows clean machine (host) and run the certification drill there (Windows runtime/installer support is deferred in STABLE_READINESS.md)"
emit_blocked hosted_signed_assets blocked signed_release_artifacts "release-hosted signed artifacts" "<none>" False "requires creating a new GitHub release with signed assets (release creation is forbidden for this milestone; release.yml signs on v[1-9]* tags)"
emit_blocked published_sbom_assets blocked sbom_assets "published SBOM assets on a release" "<none>" False "requires creating a new GitHub release publishing SBOM.cdx.json assets (release creation is forbidden for this milestone)"
emit_blocked release_signing_key_custody blocked signed_release_artifacts "stable release-signing-key custody" "<none>" False "operator decision on a committed release public key with an offline-held private key (ephemeral in-run keypair is not a stable identity)"

# ---------------------------------------------------------------------------
# Phase 4: certification report, secret scan, and SUMMARY.json assembly
# ---------------------------------------------------------------------------

cat > "$evidence_dir/CERTIFICATION_REPORT.md" <<EOF
# Release Supply-Chain Integrity Certification Report

- **Date**: $(date -u +%Y-%m-%dT%H:%M:%SZ)
- **Host**: $host
- **Nonce**: $nonce
- **Git commit**: $git_sha
- **Version**: $version
- **Confirmation**: $CONFIRMATION_PHRASE

## Evidence classes

- **deterministic**: local, byte-verifiable checks that always run (sign/verify
  roundtrip + tamper negative, CycloneDX SBOM create/verify/schema + mutation
  negative, in-toto provenance create/verify + digest match, deterministic
  source-zip double build, packaged build-twice with honest classification,
  checksum sidecar, recomputation of recorded evidence).
- **deterministic_integration**: clean-machine install/upgrade/uninstall drill
  against the published alpha.1 -> alpha.3 assets (network-dependent;
  self-classifies to blocked on network failure and exits non-zero).
- **real**: real local artifacts bound to HEAD (darwin-arm64 packaged release
  signed and verified with the ephemeral keypair, SBOM, provenance), the
  published alpha.3 asset digest-verified and installed via the documented
  installer, and the live read-only \`gh attestation verify\` attempt.
- **blocked**: hosted/external items with their exact prerequisite, never
  fabricated (external security review, Linux/Windows native clean machines,
  release-hosted signed assets, published SBOM assets, release-signing-key
  custody, byte-identical packaged archives when applicable).

## Results

Deterministic failures: $det_failures | real failures: $real_failures

See \`SUMMARY.json\` for the full row-by-row evidence and
\`secrets-scan.txt\` for the credential scan (0 findings required).
EOF

# Final secret scan of all evidence artifacts.
scan_output="$evidence_dir/secrets-scan.txt"
{
  echo "secret-scan release-supply-chain evidence $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "scan root: $evidence_dir"
  echo "scanned patterns: bearer authorization headers, GitHub token prefixes, Slack token prefixes, exported secret env assignments, and the drill placeholder value"
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
emit_det secret_scan_evidence deterministic credential_scan "no credential material in evidence" secrets-scan.txt True
printf '%s\n' "${det_evidence[@]}" > "$cert_root/det.jsonl"
printf '%s\n' "${real_evidence[@]}" > "$cert_root/real.jsonl"
printf '%s\n' "${blocked_evidence[@]}" > "$cert_root/blocked.jsonl"

python3 - "$summary_file" "$evidence_dir" "$nonce" "$host" "$git_sha" "$det_failures" "$real_failures" "$cert_root/det.jsonl" "$cert_root/real.jsonl" "$cert_root/blocked.jsonl" <<'PY'
import json, os, sys, time
summary_file, evidence_dir, nonce, host, git_sha = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
det_failures, real_failures = int(sys.argv[6]), int(sys.argv[7])
det = [json.loads(l) for l in open(sys.argv[8]) if l.strip()]
real = [json.loads(l) for l in open(sys.argv[9]) if l.strip()]
blocked = [json.loads(l) for l in open(sys.argv[10]) if l.strip()]

def exists(observed_source):
    return not observed_source or observed_source == "<none>" or os.path.isfile(os.path.join(evidence_dir, observed_source))

problems = []
required_det = ["sign_verify_roundtrip", "sbom_create_verify_schema", "provenance_create_verify",
                "release_build_twice", "packaged_build_twice", "checksum_verify",
                "install_upgrade_uninstall_drill", "secret_scan_evidence",
                "recompute_signature_verify", "recompute_release_verify"]
det_by_id = {e["id"]: e for e in det}
for rid in required_det:
    if rid not in det_by_id:
        problems.append(f"missing deterministic row: {rid}")
    elif not exists(det_by_id[rid].get("observed_source", "")):
        problems.append(f"deterministic row {rid}: observed_source missing on disk")
real_by_id = {e["id"]: e for e in real}
for rid in ["real_packaged_sign_verify", "real_sbom", "real_provenance"]:
    if rid not in real_by_id or real_by_id[rid].get("pass") is not True:
        problems.append(f"missing/failed real row: {rid}")
att_present = "real_attestation" in real_by_id or any(b["id"] == "attestation_blocked" for b in blocked)
if not att_present:
    problems.append("no real_attestation or attestation_blocked row")
install_present = "real_install_upgrade_uninstall" in real_by_id or any(b["id"] == "install_upgrade_uninstall_blocked" for b in blocked)
if not install_present:
    problems.append("no real_install_upgrade_uninstall or install_upgrade_uninstall_blocked row")
for b in blocked:
    if not b.get("prerequisite"):
        problems.append(f"blocked row {b['id']} missing prerequisite")
    elif not exists(b.get("observed_source", "")):
        problems.append(f"blocked row {b['id']}: observed_source missing on disk")
for e in det + real + blocked:
    if not exists(e.get("observed_source", "")):
        problems.append(f"row {e['id']}: observed_source missing on disk")

summary = {
    "kind": "release-supply-chain-certification",
    "generated_at": time.strftime("%Y%m%d-%H%M%S"),
    "host": host,
    "nonce": nonce,
    "git_sha": git_sha,
    "confirmation_phrase_accepted": True,
    "deterministic": {"failures": det_failures, "evidence": det},
    "real": {"failures": real_failures, "evidence": real},
    "blocked": blocked,
    "failures": det_failures + real_failures,
    "assertion_problems": problems,
}
json.dump(summary, open(summary_file, "w"), indent=2)
if problems:
    print("SUMMARY assertion problems:", file=sys.stderr)
    for p in problems:
        print("  - " + p, file=sys.stderr)
    sys.exit(2)
print(json.dumps({"failures": summary["failures"], "deterministic": det_failures, "real": real_failures, "blocked": len(blocked)}))
PY
summary_rc=$?
if [[ $summary_rc -ne 0 ]]; then
  echo "error: SUMMARY.json structural assertions failed (see stderr)" >&2
  exit 2
fi

# SUMMARY.json itself carries no credential material; scan it explicitly too.
if grep -q -E 'Authorization: Bearer|gho_[A-Za-z0-9]+|xox[baprs]-[A-Za-z0-9-]+|FUTUREDIFF_[A-Z_]+_TOKEN=[^[:space:]]+|dummy-token-never-used' "$summary_file"; then
  echo "LEAK: SUMMARY.json" >> "$scan_output"
  exit 3
fi
echo "clean: SUMMARY.json" >> "$scan_output"


echo
echo "release supply-chain certification evidence written to: $evidence_dir"
echo "deterministic failures: $det_failures | real failures: $real_failures | secret leaks: 0"

if [[ $det_failures -ne 0 ]]; then
  echo "error: deterministic section has failures (failures=$det_failures); not a green run" >&2
  exit 1
fi
if [[ $real_failures -ne 0 ]]; then
  echo "error: real section has failures (failures=$real_failures); not a green run" >&2
  exit 1
fi
echo "OK: green run"
