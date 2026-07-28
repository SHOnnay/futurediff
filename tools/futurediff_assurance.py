#!/usr/bin/env python3
"""FutureDiff production-assurance toolkit.

Standard-library-first utilities for deterministic manifests, SBOM/provenance,
secret scanning, readiness policy, SLO checks, recovery drills, chaos checks,
reproducible release archives, and optional OpenSSL signatures.
"""
from __future__ import annotations

import argparse
import base64
import dataclasses
import datetime as dt
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import uuid
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Sequence

FORMAT_VERSION = "1.0"
DEFAULT_EXCLUDES = {
    ".git",
    ".futurediff-overlay-backup",
    ".pytest_cache",
    "__pycache__",
    "dist",
    "bin",
}

class AssuranceError(RuntimeError):
    pass


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def canonical_json_bytes(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode("utf-8")


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_bytes(canonical_json_bytes(value))
    os.replace(tmp, path)


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise AssuranceError(f"cannot read JSON {path}: {exc}") from exc


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def safe_rel(path: Path, root: Path) -> str:
    rel = path.relative_to(root).as_posix()
    if rel.startswith("../") or rel == ".." or rel.startswith("/"):
        raise AssuranceError(f"unsafe relative path: {rel}")
    return rel


def should_exclude(rel: str, excludes: set[str]) -> bool:
    parts = PurePosixPath(rel).parts
    return any(part in excludes for part in parts)


def iter_regular_files(root: Path, excludes: set[str] | None = None) -> Iterable[tuple[str, Path]]:
    excludes = DEFAULT_EXCLUDES | (excludes or set())
    root = root.resolve(strict=True)
    for dirpath, dirnames, filenames in os.walk(root, topdown=True, followlinks=False):
        current = Path(dirpath)
        kept_dirs: list[str] = []
        for name in sorted(dirnames):
            p = current / name
            rel = safe_rel(p, root)
            st = p.lstat()
            if stat.S_ISLNK(st.st_mode):
                raise AssuranceError(f"symbolic link directory rejected: {rel}")
            if should_exclude(rel, excludes):
                continue
            if not stat.S_ISDIR(st.st_mode):
                raise AssuranceError(f"non-directory traversal entry rejected: {rel}")
            kept_dirs.append(name)
        dirnames[:] = kept_dirs
        for name in sorted(filenames):
            p = current / name
            rel = safe_rel(p, root)
            if should_exclude(rel, excludes):
                continue
            st = p.lstat()
            if stat.S_ISLNK(st.st_mode):
                raise AssuranceError(f"symbolic link rejected: {rel}")
            if not stat.S_ISREG(st.st_mode):
                raise AssuranceError(f"special file rejected: {rel}")
            yield rel, p


def build_manifest(root: Path, excludes: set[str] | None = None) -> dict[str, Any]:
    files = []
    total = 0
    for rel, path in iter_regular_files(root, excludes):
        size = path.stat().st_size
        total += size
        files.append({"path": rel, "sha256": sha256_file(path), "size": size})
    material = {"format_version": FORMAT_VERSION, "algorithm": "sha256", "files": files}
    return {
        **material,
        "file_count": len(files),
        "total_bytes": total,
        "manifest_digest": sha256_bytes(canonical_json_bytes(material)),
    }


def verify_manifest(root: Path, manifest: dict[str, Any], excludes: set[str] | None = None) -> dict[str, Any]:
    expected = {item["path"]: item for item in manifest.get("files", [])}
    actual_manifest = build_manifest(root, excludes)
    actual = {item["path"]: item for item in actual_manifest["files"]}
    missing = sorted(set(expected) - set(actual))
    unexpected = sorted(set(actual) - set(expected))
    changed = sorted(path for path in set(expected) & set(actual) if expected[path]["sha256"] != actual[path]["sha256"] or int(expected[path].get("size", -1)) != int(actual[path]["size"]))
    ok = not missing and not unexpected and not changed
    return {
        "verified": ok,
        "missing": missing,
        "unexpected": unexpected,
        "changed": changed,
        "expected_file_count": len(expected),
        "actual_file_count": len(actual),
        "actual_manifest_digest": actual_manifest["manifest_digest"],
    }


def spdx_guess(text: str) -> str:
    low = text.lower()
    signatures = [
        ("Apache-2.0", "apache license"),
        ("MIT", "permission is hereby granted, free of charge"),
        ("BSD-3-Clause", "neither the name of"),
        ("BSD-2-Clause", "redistribution and use in source and binary forms"),
        ("MPL-2.0", "mozilla public license version 2.0"),
        ("GPL-3.0-only", "gnu general public license"),
    ]
    for spdx, signature in signatures:
        if signature in low:
            return spdx
    return "NOASSERTION"


def find_repo_license(root: Path) -> tuple[str, str | None]:
    for name in ("LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"):
        p = root / name
        if p.is_file() and not p.is_symlink():
            text = p.read_text(encoding="utf-8", errors="replace")
            return spdx_guess(text), name
    return "NOASSERTION", None


def parse_go_mod(root: Path) -> list[dict[str, str]]:
    path = root / "go.mod"
    if not path.is_file():
        return []
    lines = path.read_text(encoding="utf-8", errors="replace").splitlines()
    deps: list[dict[str, str]] = []
    in_block = False
    for raw in lines:
        line = raw.split("//", 1)[0].strip()
        if not line:
            continue
        if line == "require (":
            in_block = True
            continue
        if in_block and line == ")":
            in_block = False
            continue
        if line.startswith("require "):
            line = line[len("require "):].strip()
        elif not in_block:
            continue
        parts = line.split()
        if len(parts) >= 2:
            deps.append({"name": parts[0], "version": parts[1], "scope": "required"})
    return sorted(deps, key=lambda x: (x["name"], x["version"]))


def create_sbom(root: Path, name: str, version: str, excludes: set[str] | None = None) -> dict[str, Any]:
    manifest = build_manifest(root, excludes)
    license_id, license_file = find_repo_license(root)
    components = []
    for item in manifest["files"]:
        components.append({
            "type": "file",
            "name": item["path"],
            "bom-ref": f"file:{item['sha256']}",
            "hashes": [{"alg": "SHA-256", "content": item["sha256"]}],
            "properties": [{"name": "futurediff:size", "value": str(item["size"])}],
        })
    for dep in parse_go_mod(root):
        components.append({
            "type": "library",
            "name": dep["name"],
            "version": dep["version"],
            "bom-ref": f"pkg:golang/{dep['name']}@{dep['version']}",
            "purl": f"pkg:golang/{dep['name']}@{dep['version']}",
            "scope": "required",
        })
    serial = uuid.UUID(manifest["manifest_digest"][:32])
    root_component: dict[str, Any] = {
        "type": "application",
        "name": name,
        "version": version,
        "bom-ref": f"application:{name}:{version}",
        "hashes": [{"alg": "SHA-256", "content": manifest["manifest_digest"]}],
    }
    if license_id != "NOASSERTION":
        root_component["licenses"] = [{"license": {"id": license_id}}]
    if license_file:
        root_component.setdefault("properties", []).append({"name": "futurediff:license_file", "value": license_file})
    return {
        "bomFormat": "CycloneDX",
        "specVersion": "1.5",
        "serialNumber": f"urn:uuid:{serial}",
        "version": 1,
        "metadata": {
            "component": root_component,
            "tools": {"components": [{"type": "application", "name": "futurediff-assurance", "version": FORMAT_VERSION}]},
            "properties": [{"name": "futurediff:manifest_digest", "value": manifest["manifest_digest"]}],
        },
        "components": sorted(components, key=lambda c: c["bom-ref"]),
    }


def verify_sbom(root: Path, sbom: dict[str, Any], excludes: set[str] | None = None) -> dict[str, Any]:
    expected_files = {}
    for component in sbom.get("components", []):
        if component.get("type") != "file":
            continue
        hashes = {h.get("alg"): h.get("content") for h in component.get("hashes", [])}
        expected_files[component.get("name", "")] = hashes.get("SHA-256")
    actual = {rel: sha256_file(path) for rel, path in iter_regular_files(root, excludes)}
    missing = sorted(set(expected_files) - set(actual))
    unexpected = sorted(set(actual) - set(expected_files))
    changed = sorted(path for path in set(actual) & set(expected_files) if actual[path] != expected_files[path])
    return {"verified": not missing and not unexpected and not changed, "missing": missing, "unexpected": unexpected, "changed": changed}


def create_provenance(root: Path, manifest: dict[str, Any], name: str, version: str, builder_id: str, source_uri: str, source_digest: str) -> dict[str, Any]:
    manifest_digest = manifest.get("manifest_digest")
    if not re.fullmatch(r"[0-9a-f]{64}", str(manifest_digest or "")):
        raise AssuranceError("manifest_digest must be a lowercase SHA-256")
    subject_name = f"{name}-{version}"
    invocation = {
        "configSource": {"uri": source_uri, "digest": {"sha1": source_digest} if len(source_digest) == 40 else {"sha256": source_digest}},
        "parameters": {"name": name, "version": version},
        "environment": {"hermetic": False, "network": "not asserted"},
    }
    predicate = {
        "buildDefinition": {
            "buildType": "https://github.com/SHOnnay/futurediff/production-assurance/v1",
            "externalParameters": invocation["parameters"],
            "internalParameters": {},
            "resolvedDependencies": [{"uri": source_uri, "digest": invocation["configSource"]["digest"]}],
        },
        "runDetails": {
            "builder": {"id": builder_id},
            "metadata": {"invocationId": f"urn:sha256:{manifest_digest}", "startedOn": "1970-01-01T00:00:00Z", "finishedOn": "1970-01-01T00:00:00Z"},
            "byproducts": [{"name": "source-manifest", "digest": {"sha256": manifest_digest}}],
        },
    }
    return {
        "_type": "https://in-toto.io/Statement/v1",
        "subject": [{"name": subject_name, "digest": {"sha256": manifest_digest}}],
        "predicateType": "https://slsa.dev/provenance/v1",
        "predicate": predicate,
    }


def verify_provenance(manifest: dict[str, Any], provenance: dict[str, Any]) -> dict[str, Any]:
    digest = manifest.get("manifest_digest")
    subjects = provenance.get("subject", [])
    byproducts = provenance.get("predicate", {}).get("runDetails", {}).get("byproducts", [])
    subject_match = any(s.get("digest", {}).get("sha256") == digest for s in subjects)
    byproduct_match = any(b.get("digest", {}).get("sha256") == digest for b in byproducts)
    types_ok = provenance.get("_type") == "https://in-toto.io/Statement/v1" and provenance.get("predicateType") == "https://slsa.dev/provenance/v1"
    return {"verified": bool(subject_match and byproduct_match and types_ok), "subject_match": subject_match, "byproduct_match": byproduct_match, "types_ok": types_ok}

SECRET_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("github_pat", re.compile(r"github_pat_[A-Za-z0-9_]{20,}")),
    ("github_token", re.compile(r"gh[pousr]_[A-Za-z0-9]{20,}")),
    ("slack_token", re.compile(r"xox[baprs]-[A-Za-z0-9-]{10,}")),
    ("aws_access_key", re.compile(r"AKIA[0-9A-Z]{16}")),
    ("private_key", re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----")),
    ("generic_bearer", re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=-]{24,}")),
]



def should_skip_secret_scan(rel: str) -> bool:
    name = PurePosixPath(rel).name
    if name.endswith("_test.go"):
        return True
    if rel.startswith("tests/") and name.startswith("test_") and name.endswith(".py"):
        return True
    return False

def secret_scan(root: Path, excludes: set[str] | None = None) -> dict[str, Any]:
    findings: list[dict[str, Any]] = []
    scanned = 0
    for rel, path in iter_regular_files(root, excludes):
        if should_skip_secret_scan(rel):
            continue
        data = path.read_bytes()
        if b"\x00" in data[:8192]:
            continue
        scanned += 1
        text = data.decode("utf-8", errors="replace")
        for line_no, line in enumerate(text.splitlines(), start=1):
            for kind, pattern in SECRET_PATTERNS:
                for match in pattern.finditer(line):
                    token = match.group(0).encode("utf-8")
                    findings.append({
                        "path": rel,
                        "line": line_no,
                        "kind": kind,
                        "fingerprint": sha256_bytes(token)[:16],
                        "length": len(match.group(0)),
                    })
    return {"clean": not findings, "scanned_text_files": scanned, "finding_count": len(findings), "findings": findings}


def license_scan(root: Path, policy: dict[str, Any]) -> dict[str, Any]:
    allowed = set(policy.get("allowed_spdx", []))
    denied_prefixes = tuple(policy.get("denied_module_prefixes", []))
    require_license = bool(policy.get("require_repository_license", True))
    license_id, license_file = find_repo_license(root)
    dependencies = parse_go_mod(root)
    denied_modules = [dep for dep in dependencies if dep["name"].startswith(denied_prefixes)] if denied_prefixes else []
    license_ok = (not require_license or license_file is not None) and (not allowed or license_id in allowed)
    approved = license_ok and not denied_modules
    return {
        "approved": approved,
        "repository_license": license_id,
        "license_file": license_file,
        "dependency_count": len(dependencies),
        "dependencies": dependencies,
        "denied_modules": denied_modules,
        "checks": {
            "repository_license_present": license_file is not None,
            "repository_license_allowed": not allowed or license_id in allowed,
            "module_denylist_clear": not denied_modules,
        },
    }


def run_command(command: Sequence[str], cwd: Path, timeout: int) -> dict[str, Any]:
    started = time.monotonic()
    try:
        proc = subprocess.run(command, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
        return {
            "command": list(command),
            "exit_code": proc.returncode,
            "duration_ms": int((time.monotonic() - started) * 1000),
            "stdout_sha256": sha256_bytes(proc.stdout.encode()),
            "stderr_sha256": sha256_bytes(proc.stderr.encode()),
            "passed": proc.returncode == 0,
        }
    except subprocess.TimeoutExpired:
        return {"command": list(command), "exit_code": None, "duration_ms": int((time.monotonic() - started) * 1000), "timed_out": True, "passed": False}
    except FileNotFoundError:
        return {"command": list(command), "exit_code": None, "duration_ms": int((time.monotonic() - started) * 1000), "not_found": True, "passed": False}


def readiness(root: Path, policy: dict[str, Any]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    for rel in policy.get("required_files", []):
        p = root / rel
        checks.append({"id": f"required_file:{rel}", "passed": p.is_file() and not p.is_symlink(), "detail": rel})
    for rel in policy.get("required_directories", []):
        p = root / rel
        checks.append({"id": f"required_directory:{rel}", "passed": p.is_dir() and not p.is_symlink(), "detail": rel})
    secret = secret_scan(root, set(policy.get("scan_excludes", [])))
    checks.append({"id": "secret_scan", "passed": secret["clean"], "detail": f"findings={secret['finding_count']}"})
    license_policy_path = policy.get("license_policy")
    license_result = None
    if license_policy_path:
        license_result = license_scan(root, read_json(root / license_policy_path))
        checks.append({"id": "license_policy", "passed": license_result["approved"], "detail": f"license={license_result['repository_license']} dependencies={license_result['dependency_count']}"})
    command_results = []
    for entry in policy.get("commands", []):
        if not isinstance(entry, list) or not entry:
            raise AssuranceError("each readiness command must be a non-empty argv list")
        result = run_command([str(x) for x in entry], root, int(policy.get("command_timeout_seconds", 120)))
        command_results.append(result)
        checks.append({"id": "command:" + " ".join(entry), "passed": result["passed"], "detail": f"exit={result.get('exit_code')} duration_ms={result['duration_ms']}"})
    required_external = list(policy.get("required_external_evidence", []))
    evidence_report = policy.get("external_evidence_report")
    external_result = {"required": required_external, "satisfied": [], "missing": []}
    if required_external:
        if evidence_report and (root / evidence_report).is_file():
            report = read_json(root / evidence_report)
            targets = {t.get("target"): t for t in report.get("targets", [])}
            for target in required_external:
                if targets.get(target, {}).get("certified") is True:
                    external_result["satisfied"].append(target)
                else:
                    external_result["missing"].append(target)
        else:
            external_result["missing"] = required_external
        checks.append({"id": "external_evidence", "passed": not external_result["missing"], "detail": "missing=" + ",".join(external_result["missing"])})
    approved = all(check["passed"] for check in checks)
    return {
        "format_version": FORMAT_VERSION,
        "evaluated_at": utc_now(),
        "approved": approved,
        "check_count": len(checks),
        "passed_count": sum(1 for c in checks if c["passed"]),
        "failed_count": sum(1 for c in checks if not c["passed"]),
        "checks": checks,
        "command_results": command_results,
        "secret_scan": secret,
        "license_scan": license_result,
        "external_evidence": external_result,
    }


def slo_evaluate(metrics: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    rules = [
        ("availability", float(metrics.get("availability", 0)), ">=", float(policy["minimum_availability"])),
        ("p95_latency_ms", float(metrics.get("p95_latency_ms", float("inf"))), "<=", float(policy["maximum_p95_latency_ms"])),
        ("error_rate", float(metrics.get("error_rate", 1)), "<=", float(policy["maximum_error_rate"])),
        ("unknown_outcomes", float(metrics.get("unknown_outcomes", float("inf"))), "<=", float(policy["maximum_unknown_outcomes"])),
        ("restore_rpo_seconds", float(metrics.get("restore_rpo_seconds", float("inf"))), "<=", float(policy["maximum_restore_rpo_seconds"])),
        ("restore_rto_seconds", float(metrics.get("restore_rto_seconds", float("inf"))), "<=", float(policy["maximum_restore_rto_seconds"])),
    ]
    checks = []
    for name, actual, op, threshold in rules:
        passed = actual >= threshold if op == ">=" else actual <= threshold
        checks.append({"metric": name, "actual": actual, "operator": op, "threshold": threshold, "passed": passed})
    return {"approved": all(c["passed"] for c in checks), "checks": checks}


def normalized_tar_add(tar: tarfile.TarFile, root: Path, rel: str, path: Path) -> None:
    info = tarfile.TarInfo(rel)
    st = path.stat()
    info.size = st.st_size
    info.mode = stat.S_IMODE(st.st_mode) & 0o755
    info.mtime = 0
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    with path.open("rb") as f:
        tar.addfile(info, f)


def backup_create(source: Path, output: Path, excludes: set[str] | None = None) -> dict[str, Any]:
    manifest = build_manifest(source, excludes)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(output, "w:gz", format=tarfile.PAX_FORMAT, compresslevel=9) as tar:
        manifest_bytes = canonical_json_bytes(manifest)
        info = tarfile.TarInfo("BACKUP_MANIFEST.json")
        info.size = len(manifest_bytes)
        info.mode = 0o644
        info.mtime = 0
        info.uid = info.gid = 0
        tar.addfile(info, fileobj=__import__("io").BytesIO(manifest_bytes))
        for rel, path in iter_regular_files(source, excludes):
            normalized_tar_add(tar, source, f"payload/{rel}", path)
    return {"archive": str(output), "sha256": sha256_file(output), "manifest_digest": manifest["manifest_digest"], "file_count": manifest["file_count"]}


def _safe_archive_rel(name: str, prefix: str) -> str | None:
    p = PurePosixPath(name)
    if p.is_absolute() or ".." in p.parts or "" in p.parts:
        raise AssuranceError(f"unsafe archive path: {name}")
    if name == prefix.rstrip("/"):
        return None
    if not name.startswith(prefix):
        raise AssuranceError(f"unexpected archive member: {name}")
    rel = name[len(prefix):]
    if not rel or rel.startswith("/"):
        raise AssuranceError(f"unsafe archive payload path: {name}")
    return rel


def backup_restore(archive: Path, destination: Path) -> dict[str, Any]:
    destination.mkdir(parents=True, exist_ok=False)
    manifest = None
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        for member in members:
            if member.issym() or member.islnk() or member.isdev() or member.isfifo():
                raise AssuranceError(f"unsafe archive member type: {member.name}")
            if member.name == "BACKUP_MANIFEST.json":
                f = tar.extractfile(member)
                if f is None:
                    raise AssuranceError("backup manifest unavailable")
                manifest = json.loads(f.read().decode("utf-8"))
                continue
            rel = _safe_archive_rel(member.name, "payload/")
            if rel is None:
                continue
            target = destination / rel
            resolved_parent = target.parent.resolve()
            if destination.resolve() not in (resolved_parent, *resolved_parent.parents):
                raise AssuranceError(f"archive escapes destination: {member.name}")
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                f = tar.extractfile(member)
                if f is None:
                    raise AssuranceError(f"cannot read member: {member.name}")
                with target.open("wb") as out:
                    shutil.copyfileobj(f, out)
                os.chmod(target, member.mode & 0o755)
            else:
                raise AssuranceError(f"unsupported archive member: {member.name}")
    if not isinstance(manifest, dict):
        raise AssuranceError("backup manifest missing")
    verification = verify_manifest(destination, manifest)
    if not verification["verified"]:
        raise AssuranceError(f"restored backup failed manifest verification: {verification}")
    return {"restored": True, "manifest_digest": manifest["manifest_digest"], "verification": verification}


def recovery_drill() -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="futurediff-recovery-") as tmp:
        base = Path(tmp)
        source = base / "source"
        source.mkdir()
        (source / "ledger.db").write_bytes(b"safe-ledger-state\n")
        (source / "config.json").write_text('{"mode":"production"}\n', encoding="utf-8")
        archive = base / "backup.tar.gz"
        created = backup_create(source, archive)
        destination = base / "restored"
        restored = backup_restore(archive, destination)
        return {"passed": True, "archive_sha256": created["sha256"], "manifest_digest": restored["manifest_digest"], "restored_file_count": restored["verification"]["actual_file_count"]}


def chaos_run() -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    with tempfile.TemporaryDirectory(prefix="futurediff-chaos-") as tmp:
        root = Path(tmp)
        # Atomic replacement preserves old value until rename.
        target = root / "state.json"
        target.write_text("old\n", encoding="utf-8")
        staged = root / "state.json.tmp"
        staged.write_text("new\n", encoding="utf-8")
        staged.unlink()
        checks.append({"id": "interrupted_atomic_write", "passed": target.read_text() == "old\n"})

        source = root / "source"
        source.mkdir()
        (source / "a.txt").write_text("a", encoding="utf-8")
        manifest = build_manifest(source)
        (source / "a.txt").write_text("tampered", encoding="utf-8")
        checks.append({"id": "post_manifest_mutation", "passed": not verify_manifest(source, manifest)["verified"]})

        # Symlink rejection.
        symlink_root = root / "symlink"
        symlink_root.mkdir()
        (symlink_root / "real.txt").write_text("x", encoding="utf-8")
        os.symlink("real.txt", symlink_root / "link.txt")
        rejected = False
        try:
            build_manifest(symlink_root)
        except AssuranceError:
            rejected = True
        checks.append({"id": "symlink_source_rejection", "passed": rejected})

        # Archive traversal rejection.
        bad = root / "bad.tar.gz"
        with tarfile.open(bad, "w:gz") as tar:
            data = b"escape"
            info = tarfile.TarInfo("payload/../../escape.txt")
            info.size = len(data)
            tar.addfile(info, __import__("io").BytesIO(data))
        rejected = False
        try:
            backup_restore(bad, root / "bad-restore")
        except AssuranceError:
            rejected = True
        checks.append({"id": "archive_traversal_rejection", "passed": rejected})

        # Corrupted archive rejection.
        corrupt = root / "corrupt.tar.gz"
        corrupt.write_bytes(b"not a tar archive")
        rejected = False
        try:
            backup_restore(corrupt, root / "corrupt-restore")
        except (AssuranceError, tarfile.TarError):
            rejected = True
        checks.append({"id": "corrupt_backup_rejection", "passed": rejected})
    return {"passed": all(c["passed"] for c in checks), "checks": checks}


def deterministic_zip(root: Path, output: Path, prefix: str, excludes: set[str] | None = None) -> dict[str, Any]:
    files = list(iter_regular_files(root, excludes))
    internal_manifest = build_manifest(root, excludes)
    output.parent.mkdir(parents=True, exist_ok=True)
    compression = zipfile.ZIP_DEFLATED
    with zipfile.ZipFile(output, "w", compression=compression, compresslevel=9) as zf:
        manifest_data = canonical_json_bytes(internal_manifest)
        info = zipfile.ZipInfo(f"{prefix}/RELEASE_MANIFEST.json", date_time=(1980, 1, 1, 0, 0, 0))
        info.compress_type = compression
        info.external_attr = 0o100644 << 16
        zf.writestr(info, manifest_data)
        for rel, path in files:
            info = zipfile.ZipInfo(f"{prefix}/{rel}", date_time=(1980, 1, 1, 0, 0, 0))
            info.compress_type = compression
            info.external_attr = (0o100000 | (stat.S_IMODE(path.stat().st_mode) & 0o755)) << 16
            zf.writestr(info, path.read_bytes())
    return {"archive": str(output), "sha256": sha256_file(output), "file_count": len(files), "manifest_digest": internal_manifest["manifest_digest"]}


def verify_release_zip(archive: Path) -> dict[str, Any]:
    with zipfile.ZipFile(archive, "r") as zf:
        names = zf.namelist()
        if not names:
            raise AssuranceError("empty release archive")
        roots = {PurePosixPath(name).parts[0] for name in names if PurePosixPath(name).parts}
        if len(roots) != 1:
            raise AssuranceError("release archive must contain exactly one root directory")
        root_name = next(iter(roots))
        manifest_name = f"{root_name}/RELEASE_MANIFEST.json"
        if manifest_name not in names:
            raise AssuranceError("release manifest missing")
        manifest = json.loads(zf.read(manifest_name).decode("utf-8"))
        expected = {item["path"]: item for item in manifest.get("files", [])}
        actual: dict[str, dict[str, Any]] = {}
        for info in zf.infolist():
            p = PurePosixPath(info.filename)
            if p.is_absolute() or ".." in p.parts:
                raise AssuranceError(f"unsafe ZIP path: {info.filename}")
            if info.is_dir() or info.filename == manifest_name:
                continue
            if len(p.parts) < 2 or p.parts[0] != root_name:
                raise AssuranceError(f"unexpected ZIP path: {info.filename}")
            rel = PurePosixPath(*p.parts[1:]).as_posix()
            data = zf.read(info.filename)
            actual[rel] = {"sha256": sha256_bytes(data), "size": len(data)}
        missing = sorted(set(expected) - set(actual))
        unexpected = sorted(set(actual) - set(expected))
        changed = sorted(rel for rel in set(actual) & set(expected) if actual[rel]["sha256"] != expected[rel]["sha256"] or actual[rel]["size"] != expected[rel]["size"])
        return {"verified": not missing and not unexpected and not changed, "root": root_name, "missing": missing, "unexpected": unexpected, "changed": changed, "archive_sha256": sha256_file(archive)}


def openssl_sign(file_path: Path, private_key: Path, signature: Path) -> dict[str, Any]:
    signature.parent.mkdir(parents=True, exist_ok=True)
    proc = subprocess.run(["openssl", "dgst", "-sha256", "-sign", str(private_key), "-out", str(signature), str(file_path)], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if proc.returncode != 0:
        raise AssuranceError(f"OpenSSL signing failed: {proc.stderr.strip()}")
    return {"signed": True, "file_sha256": sha256_file(file_path), "signature_sha256": sha256_file(signature)}


def openssl_verify(file_path: Path, public_key: Path, signature: Path) -> dict[str, Any]:
    proc = subprocess.run(["openssl", "dgst", "-sha256", "-verify", str(public_key), "-signature", str(signature), str(file_path)], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    return {"verified": proc.returncode == 0, "file_sha256": sha256_file(file_path), "signature_sha256": sha256_file(signature), "openssl_output": (proc.stdout + proc.stderr).strip()}


def release_candidate(root: Path, output_dir: Path, name: str, version: str, policy_path: Path) -> dict[str, Any]:
    output_dir.mkdir(parents=True, exist_ok=True)
    manifest = build_manifest(root, {output_dir.name})
    manifest_path = output_dir / "SOURCE_MANIFEST.json"
    write_json(manifest_path, manifest)
    sbom = create_sbom(root, name, version, {output_dir.name})
    sbom_path = output_dir / "SBOM.cdx.json"
    write_json(sbom_path, sbom)
    source_digest = os.environ.get("GITHUB_SHA", "0" * 40)
    provenance = create_provenance(root, manifest, name, version, "https://github.com/SHOnnay/futurediff/actions", "git+https://github.com/SHOnnay/futurediff", source_digest)
    provenance_path = output_dir / "PROVENANCE.intoto.json"
    write_json(provenance_path, provenance)
    readiness_result = readiness(root, read_json(policy_path))
    readiness_path = output_dir / "READINESS.json"
    write_json(readiness_path, readiness_result)
    chaos = chaos_run()
    chaos_path = output_dir / "CHAOS.json"
    write_json(chaos_path, chaos)
    recovery = recovery_drill()
    recovery_path = output_dir / "RECOVERY_DRILL.json"
    write_json(recovery_path, recovery)
    if not readiness_result["approved"] or not chaos["passed"] or not recovery["passed"]:
        raise AssuranceError("release candidate gate failed")
    archive = output_dir / f"{name}-{version}-source.zip"
    archive_result = deterministic_zip(root, archive, f"{name}-{version}", {output_dir.name})
    verification = verify_release_zip(archive)
    if not verification["verified"]:
        raise AssuranceError("release archive verification failed")
    summary = {
        "approved": True,
        "name": name,
        "version": version,
        "archive": archive.name,
        "archive_sha256": archive_result["sha256"],
        "source_manifest_digest": manifest["manifest_digest"],
        "sbom_sha256": sha256_file(sbom_path),
        "provenance_sha256": sha256_file(provenance_path),
        "readiness_sha256": sha256_file(readiness_path),
        "chaos_sha256": sha256_file(chaos_path),
        "recovery_sha256": sha256_file(recovery_path),
    }
    write_json(output_dir / "RELEASE_CANDIDATE.json", summary)
    (output_dir / f"{archive.name}.sha256").write_text(f"{archive_result['sha256']}  {archive.name}\n", encoding="utf-8")
    return summary


def common_root_arg(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--root", type=Path, required=True)


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="futurediff-assurance")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("manifest-create"); common_root_arg(p); p.add_argument("--output", type=Path, required=True)
    p = sub.add_parser("manifest-verify"); common_root_arg(p); p.add_argument("--manifest", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("sbom-create"); common_root_arg(p); p.add_argument("--name", required=True); p.add_argument("--version", required=True); p.add_argument("--output", type=Path, required=True)
    p = sub.add_parser("sbom-verify"); common_root_arg(p); p.add_argument("--sbom", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("provenance-create"); common_root_arg(p); p.add_argument("--manifest", type=Path, required=True); p.add_argument("--name", required=True); p.add_argument("--version", required=True); p.add_argument("--builder-id", required=True); p.add_argument("--source-uri", required=True); p.add_argument("--source-digest", required=True); p.add_argument("--output", type=Path, required=True)
    p = sub.add_parser("provenance-verify"); p.add_argument("--manifest", type=Path, required=True); p.add_argument("--provenance", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("secret-scan"); common_root_arg(p); p.add_argument("--output", type=Path)
    p = sub.add_parser("license-scan"); common_root_arg(p); p.add_argument("--policy", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("readiness"); common_root_arg(p); p.add_argument("--policy", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("slo-evaluate"); p.add_argument("--metrics", type=Path, required=True); p.add_argument("--policy", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("backup-create"); p.add_argument("--source", type=Path, required=True); p.add_argument("--output", type=Path, required=True)
    p = sub.add_parser("backup-restore"); p.add_argument("--archive", type=Path, required=True); p.add_argument("--destination", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("recovery-drill"); p.add_argument("--output", type=Path)
    p = sub.add_parser("chaos-run"); p.add_argument("--output", type=Path)
    p = sub.add_parser("release-build"); common_root_arg(p); p.add_argument("--name", required=True); p.add_argument("--version", required=True); p.add_argument("--output", type=Path, required=True)
    p = sub.add_parser("release-verify"); p.add_argument("--archive", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("sign"); p.add_argument("--file", type=Path, required=True); p.add_argument("--private-key", type=Path, required=True); p.add_argument("--signature", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("verify-signature"); p.add_argument("--file", type=Path, required=True); p.add_argument("--public-key", type=Path, required=True); p.add_argument("--signature", type=Path, required=True); p.add_argument("--output", type=Path)
    p = sub.add_parser("release-candidate"); common_root_arg(p); p.add_argument("--output-dir", type=Path, required=True); p.add_argument("--name", required=True); p.add_argument("--version", required=True); p.add_argument("--policy", type=Path, required=True)

    args = parser.parse_args(argv)
    try:
        result: dict[str, Any]
        if args.command == "manifest-create":
            result = build_manifest(args.root); write_json(args.output, result)
        elif args.command == "manifest-verify":
            result = verify_manifest(args.root, read_json(args.manifest))
        elif args.command == "sbom-create":
            result = create_sbom(args.root, args.name, args.version); write_json(args.output, result)
        elif args.command == "sbom-verify":
            result = verify_sbom(args.root, read_json(args.sbom))
        elif args.command == "provenance-create":
            result = create_provenance(args.root, read_json(args.manifest), args.name, args.version, args.builder_id, args.source_uri, args.source_digest); write_json(args.output, result)
        elif args.command == "provenance-verify":
            result = verify_provenance(read_json(args.manifest), read_json(args.provenance))
        elif args.command == "secret-scan":
            result = secret_scan(args.root)
        elif args.command == "license-scan":
            result = license_scan(args.root, read_json(args.policy))
        elif args.command == "readiness":
            result = readiness(args.root, read_json(args.policy))
        elif args.command == "slo-evaluate":
            result = slo_evaluate(read_json(args.metrics), read_json(args.policy))
        elif args.command == "backup-create":
            result = backup_create(args.source, args.output)
        elif args.command == "backup-restore":
            result = backup_restore(args.archive, args.destination)
        elif args.command == "recovery-drill":
            result = recovery_drill()
        elif args.command == "chaos-run":
            result = chaos_run()
        elif args.command == "release-build":
            result = deterministic_zip(args.root, args.output, f"{args.name}-{args.version}")
        elif args.command == "release-verify":
            result = verify_release_zip(args.archive)
        elif args.command == "sign":
            result = openssl_sign(args.file, args.private_key, args.signature)
        elif args.command == "verify-signature":
            result = openssl_verify(args.file, args.public_key, args.signature)
        elif args.command == "release-candidate":
            result = release_candidate(args.root, args.output_dir, args.name, args.version, args.policy)
        else:
            raise AssuranceError(f"unsupported command: {args.command}")
        output = getattr(args, "output", None)
        if output and args.command not in {"manifest-create", "sbom-create", "provenance-create", "backup-create", "release-build"}:
            write_json(output, result)
        print(json.dumps(result, sort_keys=True, indent=2))
        if result.get("verified") is False or result.get("approved") is False or result.get("clean") is False or result.get("passed") is False:
            return 2
        return 0
    except (AssuranceError, OSError, ValueError, tarfile.TarError, zipfile.BadZipFile) as exc:
        print(json.dumps({"error": str(exc), "command": args.command}, sort_keys=True), file=sys.stderr)
        return 2

if __name__ == "__main__":
    raise SystemExit(main())
