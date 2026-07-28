#!/usr/bin/env python3
"""FutureDiff production-closure assurance toolkit.

This module completes the locally implementable production-control plane while
remaining fail-closed for evidence that must be produced by real external
systems, independent reviewers, hosted runners, and production-like hosts.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import stat
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable

FORMAT_VERSION = "1.0"
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)


class ClosureError(RuntimeError):
    pass


def canonical_bytes(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode()


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ClosureError(f"cannot read JSON {path}: {exc}") from exc


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_bytes(canonical_bytes(value))
    os.replace(tmp, path)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def regular(path: Path, label: str = "file") -> Path:
    try:
        st = path.lstat()
    except OSError as exc:
        raise ClosureError(f"{label} unavailable: {path}: {exc}") from exc
    if stat.S_ISLNK(st.st_mode) or not stat.S_ISREG(st.st_mode):
        raise ClosureError(f"{label} must be a regular non-link file: {path}")
    return path


def safe_rel(value: str) -> bool:
    p = PurePosixPath(value)
    return bool(value) and not p.is_absolute() and ".." not in p.parts and "" not in p.parts


def parse_time(value: Any) -> dt.datetime:
    if not isinstance(value, str):
        raise ClosureError("timestamp must be a string")
    text = value.strip().replace("Z", "+00:00")
    try:
        parsed = dt.datetime.fromisoformat(text)
    except ValueError as exc:
        raise ClosureError(f"invalid timestamp: {value}") from exc
    if parsed.tzinfo is None:
        raise ClosureError(f"timestamp must include timezone: {value}")
    return parsed.astimezone(dt.timezone.utc)


def iso(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def check(name: str, passed: bool, detail: Any = None) -> dict[str, Any]:
    item: dict[str, Any] = {"name": name, "passed": bool(passed)}
    if detail is not None:
        item["detail"] = detail
    return item


def result(kind: str, checks: list[dict[str, Any]], **extra: Any) -> dict[str, Any]:
    passed = all(bool(x.get("passed")) for x in checks)
    value = {"format_version": FORMAT_VERSION, "kind": kind, "passed": passed, "checks": checks, **extra}
    value["result_digest"] = sha256_bytes(canonical_bytes(value))
    return value


def integration_receipt(repository: Path, base_archive: Path, manifests: list[Path]) -> dict[str, Any]:
    repo = repository.resolve(strict=True)
    base = regular(base_archive, "base archive")
    checks: list[dict[str, Any]] = []
    checks.append(check("repository_directory", repo.is_dir()))
    checks.append(check("go_mod_present", (repo / "go.mod").is_file()))
    git_head = ""
    head_path = repo / ".git" / "HEAD"
    if head_path.is_file():
        git_head = head_path.read_text(encoding="utf-8", errors="replace").strip()
    checks.append(check("repository_identity_available", bool(git_head or (repo / "go.mod").is_file())))
    items = []
    for m in manifests:
        regular(m, "overlay manifest")
        items.append({"path": str(m), "sha256": sha256_file(m)})
    checks.append(check("overlay_manifests_present", bool(items)))
    base_digest = sha256_file(base)
    marker_path = repo / ".futurediff-canonical-merge.json"
    marker = {}
    marker_valid = False
    if marker_path.is_file() and not marker_path.is_symlink():
        try:
            marker = read_json(marker_path)
            declared_manifests = {x.get("sha256") for x in marker.get("overlay_manifests", []) if isinstance(x, dict)}
            marker_valid = (
                marker.get("merged") is True
                and marker.get("base_archive_sha256") == base_digest
                and {x["sha256"] for x in items} <= declared_manifests
                and bool(marker.get("validated_at"))
            )
        except ClosureError:
            marker_valid = False
    checks.append(check("canonical_merge_receipt", marker_valid))
    return result(
        "canonical-integration-receipt",
        checks,
        repository=str(repo),
        repository_head=git_head or None,
        base_archive={"path": str(base), "sha256": base_digest, "size": base.stat().st_size},
        overlay_manifests=items,
        merged=marker_valid,
        merge_receipt=str(marker_path) if marker_path.exists() else None,
    )


def archive_catalog(root: Path, expected: dict[str, Any] | None = None) -> dict[str, Any]:
    root = root.resolve(strict=True)
    expected_map = dict((expected or {}).get("expected", {}))
    entries = []
    found = set()
    for path in sorted(root.glob("*.zip")):
        regular(path, "archive")
        digest = sha256_file(path)
        found.add(path.name)
        declared = expected_map.get(path.name)
        entries.append({
            "name": path.name,
            "sha256": digest,
            "size": path.stat().st_size,
            "expected_sha256": declared,
            "digest_matches_expected": declared is None or digest == declared,
        })
    missing = sorted(set(expected_map) - found)
    checks = [
        check("archives_present", bool(entries)),
        check("available_digests_valid", all(e["digest_matches_expected"] for e in entries)),
        check("expected_archives_complete", not missing, missing),
    ]
    return result("historical-archive-catalog", checks, archives=entries, missing_expected_archives=missing)


def freshness_plan(spec: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = now or utcnow()
    renewal_hours = float(policy.get("renew_before_hours", 24))
    rows = []
    invalid = []
    for raw in spec.get("evidence", []):
        try:
            issued = parse_time(raw["issued_at"])
            expires = parse_time(raw["expires_at"])
            if expires <= issued:
                raise ClosureError("invalid window")
            remaining = (expires - now).total_seconds() / 3600
            if remaining <= 0:
                status = "expired"
            elif remaining <= renewal_hours:
                status = "renew-now"
            else:
                status = "current"
            rows.append({"id": raw["id"], "expires_at": iso(expires), "remaining_hours": round(remaining, 3), "status": status})
        except (KeyError, ClosureError) as exc:
            invalid.append(str(raw.get("id", "<missing>")))
    checks = [check("entries_valid", not invalid, invalid), check("no_expired_evidence", not any(r["status"] == "expired" for r in rows))]
    return result("evidence-freshness-plan", checks, evidence=rows, renew_before_hours=renewal_hours)


def certification_campaign(spec: dict[str, Any]) -> dict[str, Any]:
    required = set(spec.get("required_targets", []))
    rows = spec.get("targets", [])
    ids = {str(x.get("id", "")) for x in rows if isinstance(x, dict)}
    missing = sorted(required - ids)
    invalid = []
    for row in rows:
        if not isinstance(row, dict):
            invalid.append("non-object")
            continue
        for field in ("id", "owner", "environment", "runner", "evidence_type", "command"):
            if not str(row.get(field, "")).strip():
                invalid.append(f"{row.get('id','<missing>')}:{field}")
    checks = [check("required_targets_declared", not missing, missing), check("target_records_complete", not invalid, invalid)]
    return result("external-certification-campaign", checks, targets=rows, required_targets=sorted(required))


def security_review(review: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    reviewer = review.get("reviewer", {})
    findings = review.get("findings", [])
    max_open = {str(x) for x in policy.get("allowed_open_severities", ["low", "informational"])}
    required_scope = set(policy.get("required_scope", []))
    scope = set(review.get("scope", []))
    unresolved = [f for f in findings if str(f.get("status", "open")) != "resolved" and str(f.get("severity", "")).lower() not in max_open]
    checks = [
        check("non_synthetic", review.get("synthetic") is False),
        check("independent_reviewer", bool(reviewer.get("independent")) and reviewer.get("organization") != review.get("subject_organization")),
        check("required_scope_covered", required_scope <= scope, sorted(required_scope - scope)),
        check("report_digest_present", isinstance(review.get("report_sha256"), str) and len(review.get("report_sha256", "")) == 64),
        check("no_disallowed_open_findings", not unresolved, [f.get("id") for f in unresolved]),
        check("review_signed", bool(review.get("signed_by")) and bool(review.get("signed_at"))),
    ]
    return result("independent-security-review", checks, unresolved_findings=unresolved, reviewer=reviewer)


def load_soak(evidence: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    metrics = evidence.get("metrics", {})
    checks = [
        check("non_synthetic", evidence.get("synthetic") is False),
        check("duration", float(evidence.get("duration_hours", 0)) >= float(policy.get("min_duration_hours", 24))),
        check("requests", int(evidence.get("request_count", 0)) >= int(policy.get("min_request_count", 10000))),
        check("error_rate", float(metrics.get("error_rate", 1)) <= float(policy.get("max_error_rate", 0.001))),
        check("p95_latency_ms", float(metrics.get("p95_latency_ms", 1e18)) <= float(policy.get("max_p95_latency_ms", 500))),
        check("memory_growth_pct", float(metrics.get("memory_growth_pct", 1e18)) <= float(policy.get("max_memory_growth_pct", 5))),
        check("unknown_outcomes", int(metrics.get("unknown_outcomes", 1)) <= int(policy.get("max_unknown_outcomes", 0))),
        check("evidence_digest", len(str(evidence.get("evidence_sha256", ""))) == 64),
    ]
    return result("measured-load-soak-evidence", checks, evidence=evidence)


def dr_evidence(evidence: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    checks = [
        check("non_synthetic", evidence.get("synthetic") is False),
        check("restore_success", evidence.get("restore_success") is True),
        check("rto", float(evidence.get("measured_rto_minutes", 1e18)) <= float(policy.get("max_rto_minutes", 60))),
        check("rpo", float(evidence.get("measured_rpo_minutes", 1e18)) <= float(policy.get("max_rpo_minutes", 5))),
        check("integrity_verified", evidence.get("integrity_verified") is True),
        check("exercise_timestamp", bool(evidence.get("executed_at"))),
        check("evidence_digest", len(str(evidence.get("evidence_sha256", ""))) == 64),
    ]
    return result("disaster-recovery-evidence", checks, evidence=evidence)


def change_control(value: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = now or utcnow()
    try:
        starts = parse_time(value["freeze_starts_at"])
        ends = parse_time(value["freeze_ends_at"])
        valid_window = ends > starts
    except (KeyError, ClosureError):
        starts = ends = now
        valid_window = False
    approvals = {str(a.get("role")) for a in value.get("approvals", []) if a.get("approved") is True}
    required_roles = set(policy.get("required_roles", []))
    checks = [
        check("valid_freeze_window", valid_window),
        check("release_identifier", bool(value.get("release_id"))),
        check("change_list_digest", len(str(value.get("change_list_sha256", ""))) == 64),
        check("required_approvals", required_roles <= approvals, sorted(required_roles - approvals)),
        check("emergency_override_absent", not bool(value.get("emergency_override", False))),
    ]
    return result("change-freeze-control", checks, freeze_active=valid_window and starts <= now <= ends)


SECRET_KEYS = {"secret", "token", "password", "private_key", "credential_value", "api_key"}


def credential_readiness(value: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = now or utcnow()
    violations = []
    expired = []
    scope_failures = []
    required = set(policy.get("required_credentials", []))
    present = set()
    for row in value.get("credentials", []):
        cid = str(row.get("id", ""))
        present.add(cid)
        if any(k in row for k in SECRET_KEYS):
            violations.append(cid)
        try:
            expires = parse_time(row["expires_at"])
            if expires <= now:
                expired.append(cid)
        except (KeyError, ClosureError):
            expired.append(cid)
        if not row.get("broker") or not row.get("rotation_owner") or not row.get("scopes"):
            scope_failures.append(cid)
    checks = [
        check("required_credential_metadata", required <= present, sorted(required - present)),
        check("no_secret_values", not violations, violations),
        check("credentials_current", not expired, expired),
        check("broker_rotation_and_scope", not scope_failures, scope_failures),
    ]
    return result("production-credential-readiness", checks, credential_count=len(present))


def smoke_test(value: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    required = set(policy.get("required_checks", []))
    rows = value.get("checks", [])
    passed = {str(x.get("id")) for x in rows if x.get("passed") is True and x.get("synthetic") is False}
    checks = [
        check("production_environment", value.get("environment") == policy.get("environment", "production-like")),
        check("archive_digest_bound", len(str(value.get("archive_sha256", ""))) == 64),
        check("required_smoke_checks", required <= passed, sorted(required - passed)),
        check("no_failed_checks", not any(x.get("passed") is False for x in rows)),
        check("evidence_digest", len(str(value.get("evidence_sha256", ""))) == 64),
    ]
    return result("deployment-smoke-test", checks, required_checks=sorted(required))


def rollback_exercise(value: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    checks = [
        check("non_synthetic", value.get("synthetic") is False),
        check("rollback_triggered", value.get("triggered") is True),
        check("rollback_success", value.get("success") is True),
        check("rollback_time", float(value.get("duration_minutes", 1e18)) <= float(policy.get("max_duration_minutes", 15))),
        check("state_integrity", value.get("state_integrity_verified") is True),
        check("forward_recovery", value.get("forward_recovery_verified") is True),
        check("evidence_digest", len(str(value.get("evidence_sha256", ""))) == 64),
    ]
    return result("rollback-exercise", checks, exercise=value)


def operational_signoff(value: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    required_roles = set(policy.get("required_roles", []))
    minimum = int(policy.get("minimum_distinct_approvers", len(required_roles)))
    approvals = [x for x in value.get("approvals", []) if x.get("approved") is True]
    actors = {str(x.get("actor")) for x in approvals}
    roles = {str(x.get("role")) for x in approvals}
    self_approvals = [x for x in approvals if x.get("actor") == value.get("release_owner")]
    checks = [
        check("release_digest_bound", len(str(value.get("release_sha256", ""))) == 64),
        check("required_roles", required_roles <= roles, sorted(required_roles - roles)),
        check("distinct_approvers", len(actors) >= minimum, sorted(actors)),
        check("release_owner_not_self_approving", not self_approvals),
        check("on_call_confirmed", value.get("on_call_confirmed") is True),
        check("communications_ready", value.get("communications_ready") is True),
    ]
    return result("operational-signoff", checks, approvers=sorted(actors), roles=sorted(roles))


def completion_decision(results: list[dict[str, Any]], required_kinds: set[str]) -> dict[str, Any]:
    by_kind = {str(r.get("kind")): r for r in results}
    missing = sorted(required_kinds - set(by_kind))
    failed = sorted(k for k in required_kinds if k in by_kind and by_kind[k].get("passed") is not True)
    synthetic = sorted(k for k, r in by_kind.items() if r.get("synthetic") is True)
    checks = [
        check("all_required_results_present", not missing, missing),
        check("all_required_results_passed", not failed, failed),
        check("no_synthetic_result_override", not synthetic, synthetic),
    ]
    digests = {k: by_kind[k].get("result_digest") for k in sorted(by_kind) if k in required_kinds}
    return result(
        "production-completion-decision",
        checks,
        production_complete=all(c["passed"] for c in checks),
        result_digests=digests,
        missing=missing,
        failed=failed,
    )


def deterministic_bundle(output: Path, root: Path, files: Iterable[str]) -> dict[str, Any]:
    root = root.resolve(strict=True)
    members = []
    for rel in sorted(set(files)):
        if not safe_rel(rel):
            raise ClosureError(f"unsafe bundle path: {rel}")
        path = regular(root / rel, "bundle member")
        members.append((rel, path, sha256_file(path)))
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
        manifest = {"format_version": FORMAT_VERSION, "members": [{"path": r, "sha256": d, "size": p.stat().st_size} for r, p, d in members]}
        info = zipfile.ZipInfo("BUNDLE_MANIFEST.json", FIXED_ZIP_TIME)
        info.external_attr = 0o100644 << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        zf.writestr(info, canonical_bytes(manifest))
        for rel, path, _ in members:
            info = zipfile.ZipInfo(rel, FIXED_ZIP_TIME)
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            zf.writestr(info, path.read_bytes())
    return {"path": str(output), "sha256": sha256_file(output), "size": output.stat().st_size, "member_count": len(members)}


def verify_bundle(path: Path) -> dict[str, Any]:
    regular(path, "bundle")
    errors = []
    try:
        with zipfile.ZipFile(path) as zf:
            names = zf.namelist()
            for name in names:
                if not safe_rel(name):
                    errors.append(f"unsafe:{name}")
            manifest = json.loads(zf.read("BUNDLE_MANIFEST.json"))
            declared = {x["path"]: x for x in manifest.get("members", [])}
            actual_names = set(names) - {"BUNDLE_MANIFEST.json"}
            if actual_names != set(declared):
                errors.append("member-set-mismatch")
            for name, item in declared.items():
                if name in actual_names and sha256_bytes(zf.read(name)) != item.get("sha256"):
                    errors.append(f"digest:{name}")
    except (OSError, zipfile.BadZipFile, KeyError, json.JSONDecodeError) as exc:
        errors.append(str(exc))
    return result("closure-bundle-verification", [check("bundle_valid", not errors, errors)], bundle_sha256=sha256_file(path))


def parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="command", required=True)
    def io(name: str, *args: str):
        q = sub.add_parser(name)
        for arg in args:
            q.add_argument(f"--{arg.replace('_','-')}", required=True)
        q.add_argument("--output", required=True)
        return q
    q = io("integration-receipt", "repository", "base_archive")
    q.add_argument("--manifest", action="append", required=True)
    io("archive-catalog", "root", "expected")
    io("freshness-plan", "spec", "policy")
    io("certification-campaign", "spec")
    io("security-review", "review", "policy")
    io("load-soak", "evidence", "policy")
    io("dr-evidence", "evidence", "policy")
    io("change-control", "record", "policy")
    io("credential-readiness", "record", "policy")
    io("smoke-test", "record", "policy")
    io("rollback-exercise", "record", "policy")
    io("operational-signoff", "record", "policy")
    q = sub.add_parser("completion-decision")
    q.add_argument("--result", action="append", required=True)
    q.add_argument("--required-kind", action="append", required=True)
    q.add_argument("--output", required=True)
    q = sub.add_parser("bundle")
    q.add_argument("--root", required=True)
    q.add_argument("--file", action="append", required=True)
    q.add_argument("--output", required=True)
    q = sub.add_parser("verify-bundle")
    q.add_argument("--bundle", required=True)
    q.add_argument("--output", required=True)
    return p


def main() -> int:
    args = parser().parse_args()
    try:
        c = args.command
        if c == "integration-receipt":
            value = integration_receipt(Path(args.repository), Path(args.base_archive), [Path(x) for x in args.manifest])
        elif c == "archive-catalog":
            value = archive_catalog(Path(args.root), read_json(Path(args.expected)))
        elif c == "freshness-plan":
            value = freshness_plan(read_json(Path(args.spec)), read_json(Path(args.policy)))
        elif c == "certification-campaign":
            value = certification_campaign(read_json(Path(args.spec)))
        elif c == "security-review":
            value = security_review(read_json(Path(args.review)), read_json(Path(args.policy)))
        elif c == "load-soak":
            value = load_soak(read_json(Path(args.evidence)), read_json(Path(args.policy)))
        elif c == "dr-evidence":
            value = dr_evidence(read_json(Path(args.evidence)), read_json(Path(args.policy)))
        elif c == "change-control":
            value = change_control(read_json(Path(args.record)), read_json(Path(args.policy)))
        elif c == "credential-readiness":
            value = credential_readiness(read_json(Path(args.record)), read_json(Path(args.policy)))
        elif c == "smoke-test":
            value = smoke_test(read_json(Path(args.record)), read_json(Path(args.policy)))
        elif c == "rollback-exercise":
            value = rollback_exercise(read_json(Path(args.record)), read_json(Path(args.policy)))
        elif c == "operational-signoff":
            value = operational_signoff(read_json(Path(args.record)), read_json(Path(args.policy)))
        elif c == "completion-decision":
            value = completion_decision([read_json(Path(x)) for x in args.result], set(args.required_kind))
        elif c == "bundle":
            value = deterministic_bundle(Path(args.output), Path(args.root), args.file)
            print(json.dumps(value, sort_keys=True))
            return 0
        elif c == "verify-bundle":
            value = verify_bundle(Path(args.bundle))
        else:
            raise ClosureError(f"unknown command: {c}")
        write_json(Path(args.output), value)
        print(json.dumps(value, sort_keys=True))
        return 0 if value.get("passed", True) else 1
    except ClosureError as exc:
        print(f"error: {exc}", file=os.sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
