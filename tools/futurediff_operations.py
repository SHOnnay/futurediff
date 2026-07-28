#!/usr/bin/env python3
"""FutureDiff operational and deployment assurance toolkit.

Standard-library-only validators for deployment contracts, compatibility,
upgrade/rollback safety, capacity and soak evidence, observability, alerting,
data governance, incident tabletop exercises, release approval quorum,
evidence catalogs, deterministic certification bundles, and a final local gate.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import stat
import sys
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Sequence

FORMAT_VERSION = "1.0"
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)


class OperationsError(RuntimeError):
    pass


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def canonical_json_bytes(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode("utf-8")


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise OperationsError(f"cannot read JSON {path}: {exc}") from exc


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_bytes(canonical_json_bytes(value))
    os.replace(tmp, path)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def is_sha256(value: Any) -> bool:
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def ensure_regular(path: Path, *, label: str = "file") -> Path:
    try:
        st = path.lstat()
    except OSError as exc:
        raise OperationsError(f"{label} unavailable: {path}: {exc}") from exc
    if stat.S_ISLNK(st.st_mode) or not stat.S_ISREG(st.st_mode):
        raise OperationsError(f"{label} must be a regular file: {path}")
    return path


def safe_relative(path: str) -> bool:
    p = PurePosixPath(path)
    return bool(path) and not p.is_absolute() and ".." not in p.parts and "" not in p.parts


def result(kind: str, passed: bool, checks: list[dict[str, Any]], **extra: Any) -> dict[str, Any]:
    return {
        "format_version": FORMAT_VERSION,
        "kind": kind,
        "generated_at": utc_now(),
        "passed": bool(passed),
        "checks": checks,
        **extra,
    }


def check(name: str, passed: bool, detail: Any = None) -> dict[str, Any]:
    item: dict[str, Any] = {"name": name, "passed": bool(passed)}
    if detail is not None:
        item["detail"] = detail
    return item


def validate_deployment_contract(contract: dict[str, Any]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    service = contract.get("service", {})
    envs = contract.get("environments", {})
    required = ["staging", "production"]
    checks.append(check("format_version", contract.get("format_version") == FORMAT_VERSION))
    checks.append(check("service_name", isinstance(service.get("name"), str) and bool(service.get("name", "").strip())))
    checks.append(check("service_version", isinstance(service.get("version"), str) and bool(service.get("version", "").strip())))
    checks.append(check("service_owner", isinstance(service.get("owner"), str) and "@" in service.get("owner", "")))
    checks.append(check("required_environments", all(name in envs for name in required), {"required": required}))

    secret_keys = re.compile(r"(?i)(password|secret|token|private[_-]?key|api[_-]?key)")
    forbidden_values: list[str] = []
    for env_name, env in envs.items() if isinstance(envs, dict) else []:
        if not isinstance(env, dict):
            checks.append(check(f"environment_{env_name}_object", False))
            continue
        checks.extend([
            check(f"{env_name}_region", isinstance(env.get("region"), str) and bool(env.get("region", "").strip())),
            check(f"{env_name}_replicas", isinstance(env.get("replicas"), int) and env.get("replicas", 0) >= (2 if env_name == "production" else 1)),
            check(f"{env_name}_database", isinstance(env.get("database"), dict) and bool(env.get("database", {}).get("engine"))),
            check(f"{env_name}_queue", isinstance(env.get("queue"), dict) and bool(env.get("queue", {}).get("engine"))),
            check(f"{env_name}_secret_provider", env.get("secret_provider") in {"environment", "vault", "cloud-kms", "secret-manager"}),
            check(f"{env_name}_observability", isinstance(env.get("observability"), dict) and all(bool(env.get("observability", {}).get(k)) for k in ("metrics", "logs", "traces"))),
            check(f"{env_name}_backup", isinstance(env.get("backup"), dict) and int(env.get("backup", {}).get("rpo_seconds", -1)) >= 0 and int(env.get("backup", {}).get("rto_seconds", -1)) >= 0),
        ])
        for key, value in env.items():
            if key == "secret_provider":
                continue
            if secret_keys.search(str(key)) and isinstance(value, str) and value not in {"", "REDACTED", "REFERENCE_ONLY"}:
                forbidden_values.append(f"{env_name}.{key}")
    checks.append(check("no_embedded_secrets", not forbidden_values, forbidden_values))
    passed = all(c["passed"] for c in checks)
    return result("deployment_contract", passed, checks, contract_digest=sha256_bytes(canonical_json_bytes(contract)))


def environment_parity(contract: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    base = validate_deployment_contract(contract)
    checks = [check("contract_valid", base["passed"])]
    envs = contract.get("environments", {})
    staging = envs.get("staging", {}) if isinstance(envs, dict) else {}
    production = envs.get("production", {}) if isinstance(envs, dict) else {}
    required_equal = policy.get("required_equal_fields", [])
    allowed_different = set(policy.get("allowed_different_fields", []))
    mismatches: list[dict[str, Any]] = []
    for field in required_equal:
        if staging.get(field) != production.get(field):
            mismatches.append({"field": field, "staging": staging.get(field), "production": production.get(field)})
    checks.append(check("required_equal_fields", not mismatches, mismatches))
    unknown = sorted((set(staging) ^ set(production)) - allowed_different)
    checks.append(check("environment_key_parity", not unknown, unknown))
    checks.append(check("production_replica_floor", isinstance(production.get("replicas"), int) and production.get("replicas", 0) >= int(policy.get("minimum_production_replicas", 2))))
    passed = all(c["passed"] for c in checks)
    return result("environment_parity", passed, checks, contract_digest=base["contract_digest"])


def compatibility_validate(matrix: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    rows = matrix.get("results", [])
    required = policy.get("required_combinations", [])
    checks: list[dict[str, Any]] = []
    index: dict[tuple[str, str, str], dict[str, Any]] = {}
    duplicates: list[tuple[str, str, str]] = []
    invalid_evidence: list[tuple[str, str, str]] = []
    for row in rows if isinstance(rows, list) else []:
        key = (str(row.get("os")), str(row.get("runtime")), str(row.get("database")))
        if key in index:
            duplicates.append(key)
        index[key] = row
        if row.get("status") == "passed" and not is_sha256(row.get("evidence_sha256")):
            invalid_evidence.append(key)
    missing = []
    failed = []
    for item in required:
        key = (str(item.get("os")), str(item.get("runtime")), str(item.get("database")))
        row = index.get(key)
        if row is None:
            missing.append(key)
        elif row.get("status") != "passed":
            failed.append(key)
    checks.append(check("unique_combinations", not duplicates, duplicates))
    checks.append(check("required_combinations_present", not missing, missing))
    checks.append(check("required_combinations_passed", not failed, failed))
    checks.append(check("passed_evidence_digest", not invalid_evidence, invalid_evidence))
    return result("compatibility_matrix", all(c["passed"] for c in checks), checks, result_count=len(index))


def upgrade_plan_validate(plan: dict[str, Any]) -> dict[str, Any]:
    steps = plan.get("steps", [])
    ids = [str(s.get("id")) for s in steps if isinstance(s, dict)] if isinstance(steps, list) else []
    kinds = [str(s.get("kind")) for s in steps if isinstance(s, dict)] if isinstance(steps, list) else []
    checks = [
        check("version_transition", bool(plan.get("from_version")) and bool(plan.get("to_version")) and plan.get("from_version") != plan.get("to_version")),
        check("steps_present", isinstance(steps, list) and len(steps) >= 4),
        check("step_ids_unique", len(ids) == len(set(ids)) and all(ids)),
        check("backup_step", "backup" in kinds),
        check("migration_or_deploy_step", any(k in kinds for k in ("migration", "deploy"))),
        check("verification_step", "verify" in kinds),
        check("rollback_step", "rollback" in kinds),
    ]
    invalid: list[str] = []
    for step in steps if isinstance(steps, list) else []:
        if not isinstance(step, dict):
            invalid.append("non-object")
            continue
        if int(step.get("timeout_seconds", 0)) <= 0:
            invalid.append(f"{step.get('id')}:timeout")
        if step.get("kind") in {"migration", "deploy"} and not step.get("rollback_step"):
            invalid.append(f"{step.get('id')}:rollback")
        if step.get("destructive") is True and step.get("backup_required") is not True:
            invalid.append(f"{step.get('id')}:backup_required")
    checks.append(check("step_safety", not invalid, invalid))
    return result("upgrade_plan", all(c["passed"] for c in checks), checks, plan_digest=sha256_bytes(canonical_json_bytes(plan)))


def rollback_drill(plan: dict[str, Any]) -> dict[str, Any]:
    validation = upgrade_plan_validate(plan)
    checks = [check("plan_valid", validation["passed"])]
    steps = {str(s.get("id")): s for s in plan.get("steps", []) if isinstance(s, dict)}
    exercised: list[str] = []
    missing: list[str] = []
    for step in plan.get("steps", []):
        if not isinstance(step, dict) or step.get("kind") not in {"migration", "deploy"}:
            continue
        target = str(step.get("rollback_step", ""))
        rollback = steps.get(target)
        if not rollback or rollback.get("kind") != "rollback":
            missing.append(str(step.get("id")))
        else:
            exercised.append(target)
    checks.append(check("rollback_paths_complete", not missing, missing))
    checks.append(check("rollback_exercised", bool(exercised), exercised))
    checks.append(check("state_restored", validation["passed"] and not missing))
    return result("rollback_drill", all(c["passed"] for c in checks), checks, plan_digest=validation["plan_digest"])


def threshold_evaluate(kind: str, evidence: dict[str, Any], policy: dict[str, Any], rules: list[tuple[str, str]]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    for field, direction in rules:
        value = evidence.get(field)
        threshold = policy.get(field)
        ok = isinstance(value, (int, float)) and isinstance(threshold, (int, float))
        if ok:
            ok = value >= threshold if direction == "min" else value <= threshold
        checks.append(check(field, ok, {"actual": value, "threshold": threshold, "direction": direction}))
    checks.append(check("evidence_id", isinstance(evidence.get("evidence_id"), str) and bool(evidence.get("evidence_id"))))
    checks.append(check("source_digest", is_sha256(evidence.get("source_digest"))))
    return result(kind, all(c["passed"] for c in checks), checks, evidence_digest=sha256_bytes(canonical_json_bytes(evidence)))


def capacity_evaluate(evidence: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    rules = [
        ("duration_seconds", "min"), ("request_count", "min"), ("concurrency", "min"),
        ("throughput_per_second", "min"), ("p95_latency_ms", "max"), ("p99_latency_ms", "max"),
        ("error_rate", "max"), ("cpu_peak_percent", "max"), ("memory_peak_percent", "max"),
        ("unknown_outcomes", "max"),
    ]
    return threshold_evaluate("capacity_test", evidence, policy, rules)


def soak_evaluate(evidence: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    rules = [
        ("duration_seconds", "min"), ("transaction_count", "min"), ("error_rate", "max"),
        ("memory_growth_mb_per_hour", "max"), ("fd_growth_per_hour", "max"),
        ("queue_lag_peak_seconds", "max"), ("unknown_outcomes", "max"),
    ]
    return threshold_evaluate("soak_test", evidence, policy, rules)


def observability_validate(contract: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    metrics = set(contract.get("metrics", []))
    log_fields = set(contract.get("log_fields", []))
    spans = set(contract.get("trace_spans", []))
    checks.append(check("required_metrics", not (set(policy.get("required_metrics", [])) - metrics), sorted(set(policy.get("required_metrics", [])) - metrics)))
    checks.append(check("required_log_fields", not (set(policy.get("required_log_fields", [])) - log_fields), sorted(set(policy.get("required_log_fields", [])) - log_fields)))
    checks.append(check("required_trace_spans", not (set(policy.get("required_trace_spans", [])) - spans), sorted(set(policy.get("required_trace_spans", [])) - spans)))
    forbidden = set(policy.get("forbidden_log_fields", [])) & log_fields
    checks.append(check("forbidden_log_fields_absent", not forbidden, sorted(forbidden)))
    checks.append(check("retention_declared", isinstance(contract.get("retention_days"), int) and contract.get("retention_days", 0) > 0))
    checks.append(check("sampling_declared", isinstance(contract.get("trace_sampling_ratio"), (int, float)) and 0 < contract.get("trace_sampling_ratio", 0) <= 1))
    return result("observability_contract", all(c["passed"] for c in checks), checks, contract_digest=sha256_bytes(canonical_json_bytes(contract)))


def alert_routing_validate(routes: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    entries = routes.get("routes", [])
    required = set(policy.get("required_severities", []))
    found: set[str] = set()
    invalid: list[str] = []
    max_ack = policy.get("maximum_ack_minutes", {})
    for route in entries if isinstance(entries, list) else []:
        severity = str(route.get("severity", ""))
        found.add(severity)
        if not route.get("primary") or not route.get("secondary") or route.get("primary") == route.get("secondary"):
            invalid.append(f"{severity}:responders")
        limit = max_ack.get(severity)
        if not isinstance(route.get("ack_minutes"), int) or not isinstance(limit, int) or route.get("ack_minutes") > limit:
            invalid.append(f"{severity}:ack")
        if not route.get("escalation_after_minutes") or route.get("escalation_after_minutes") <= route.get("ack_minutes", 0):
            invalid.append(f"{severity}:escalation")
    checks = [
        check("required_severities", not (required - found), sorted(required - found)),
        check("route_integrity", not invalid, invalid),
        check("test_timestamp", isinstance(routes.get("last_tested_at"), str) and routes.get("last_tested_at", "").endswith("Z")),
    ]
    return result("alert_routing", all(c["passed"] for c in checks), checks)


def data_governance_validate(policy: dict[str, Any]) -> dict[str, Any]:
    classes = policy.get("data_classes", [])
    names: list[str] = []
    invalid: list[str] = []
    for item in classes if isinstance(classes, list) else []:
        name = str(item.get("name", ""))
        names.append(name)
        if int(item.get("retention_days", -1)) < 0:
            invalid.append(f"{name}:retention")
        if not item.get("deletion_method"):
            invalid.append(f"{name}:deletion")
        if item.get("contains_credentials") and item.get("storage") != "credential_broker_only":
            invalid.append(f"{name}:credential_storage")
        if item.get("contains_personal_data") and not item.get("legal_basis"):
            invalid.append(f"{name}:legal_basis")
    checks = [
        check("classes_present", isinstance(classes, list) and bool(classes)),
        check("class_names_unique", len(names) == len(set(names)) and all(names)),
        check("class_controls", not invalid, invalid),
        check("deletion_verification", policy.get("deletion_verification_required") is True),
        check("backup_expiry", isinstance(policy.get("backup_max_retention_days"), int) and policy.get("backup_max_retention_days", 0) > 0),
    ]
    return result("data_governance", all(c["passed"] for c in checks), checks, policy_digest=sha256_bytes(canonical_json_bytes(policy)))


def incident_tabletop_evaluate(exercise: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    scenarios = exercise.get("scenarios", [])
    required = set(policy.get("required_scenarios", []))
    found = {str(s.get("type")) for s in scenarios if isinstance(s, dict)} if isinstance(scenarios, list) else set()
    incomplete: list[str] = []
    for scenario in scenarios if isinstance(scenarios, list) else []:
        if not isinstance(scenario, dict):
            incomplete.append("non-object")
            continue
        for field in ("detection", "containment", "recovery", "communications", "lessons"):
            if not scenario.get(field):
                incomplete.append(f"{scenario.get('type')}:{field}")
        if int(scenario.get("score", 0)) < int(policy.get("minimum_scenario_score", 80)):
            incomplete.append(f"{scenario.get('type')}:score")
    checks = [
        check("required_scenarios", not (required - found), sorted(required - found)),
        check("scenario_completeness", not incomplete, incomplete),
        check("participants", isinstance(exercise.get("participants"), list) and len(set(exercise.get("participants", []))) >= int(policy.get("minimum_participants", 3))),
        check("action_owner", all(bool(a.get("owner")) and bool(a.get("due_date")) for a in exercise.get("actions", []) if isinstance(a, dict))),
    ]
    return result("incident_tabletop", all(c["passed"] for c in checks), checks, exercise_digest=sha256_bytes(canonical_json_bytes(exercise)))


def approvals_validate(record: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    approvals = record.get("approvals", [])
    digest = record.get("release_digest")
    valid = []
    invalid: list[str] = []
    seen: set[str] = set()
    roles: set[str] = set()
    for approval in approvals if isinstance(approvals, list) else []:
        actor = str(approval.get("actor", ""))
        role = str(approval.get("role", ""))
        if not actor or actor in seen:
            invalid.append(f"duplicate_or_missing:{actor}")
            continue
        seen.add(actor)
        roles.add(role)
        if approval.get("release_digest") != digest or not is_sha256(approval.get("approval_digest")):
            invalid.append(f"digest:{actor}")
            continue
        if actor == record.get("requested_by") and policy.get("disallow_self_approval", True):
            invalid.append(f"self:{actor}")
            continue
        valid.append(approval)
    required_roles = set(policy.get("required_roles", []))
    checks = [
        check("release_digest", is_sha256(digest)),
        check("approval_quorum", len(valid) >= int(policy.get("minimum_approvals", 2)), {"valid": len(valid)}),
        check("required_roles", not (required_roles - roles), sorted(required_roles - roles)),
        check("approval_integrity", not invalid, invalid),
    ]
    return result("release_approvals", all(c["passed"] for c in checks), checks, release_digest=digest)


def evidence_catalog(root: Path, specification: dict[str, Any]) -> dict[str, Any]:
    root = root.resolve(strict=True)
    entries: list[dict[str, Any]] = []
    checks: list[dict[str, Any]] = []
    missing: list[str] = []
    invalid: list[str] = []
    for item in specification.get("evidence", []):
        rel = str(item.get("path", ""))
        if not safe_relative(rel):
            invalid.append(rel)
            continue
        path = root / rel
        if not path.exists():
            if item.get("required", True):
                missing.append(rel)
            continue
        try:
            ensure_regular(path, label="evidence")
        except OperationsError:
            invalid.append(rel)
            continue
        entries.append({
            "id": str(item.get("id", rel)),
            "type": str(item.get("type", "unspecified")),
            "path": rel,
            "sha256": sha256_file(path),
            "size": path.stat().st_size,
            "required": bool(item.get("required", True)),
        })
    entries.sort(key=lambda x: (x["id"], x["path"]))
    checks.append(check("required_evidence_present", not missing, missing))
    checks.append(check("safe_regular_evidence", not invalid, invalid))
    checks.append(check("unique_evidence_ids", len({e["id"] for e in entries}) == len(entries)))
    material = {"format_version": FORMAT_VERSION, "entries": entries}
    return result("evidence_catalog", all(c["passed"] for c in checks), checks, entries=entries, catalog_digest=sha256_bytes(canonical_json_bytes(material)))


def certification_bundle(root: Path, catalog: dict[str, Any], output: Path, prefix: str) -> dict[str, Any]:
    if not catalog.get("passed"):
        raise OperationsError("refusing to bundle a failed evidence catalog")
    if not safe_relative(prefix):
        raise OperationsError("unsafe bundle prefix")
    output.parent.mkdir(parents=True, exist_ok=True)
    root = root.resolve(strict=True)
    catalog_bytes = canonical_json_bytes(catalog)
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9, strict_timestamps=True) as zf:
        info = zipfile.ZipInfo(f"{prefix}/EVIDENCE_CATALOG.json", FIXED_ZIP_TIME)
        info.external_attr = 0o100644 << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        zf.writestr(info, catalog_bytes)
        for entry in catalog.get("entries", []):
            rel = entry["path"]
            path = ensure_regular(root / rel, label="catalog evidence")
            actual = sha256_file(path)
            if actual != entry["sha256"]:
                raise OperationsError(f"evidence changed after cataloging: {rel}")
            info = zipfile.ZipInfo(f"{prefix}/evidence/{rel}", FIXED_ZIP_TIME)
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            zf.writestr(info, path.read_bytes())
    return verify_certification_bundle(output)


def verify_certification_bundle(archive: Path) -> dict[str, Any]:
    ensure_regular(archive, label="bundle")
    checks: list[dict[str, Any]] = []
    errors: list[str] = []
    try:
        with zipfile.ZipFile(archive, "r") as zf:
            names = zf.namelist()
            if not names:
                errors.append("empty")
                catalog = {}
            else:
                prefixes = {PurePosixPath(n).parts[0] for n in names if PurePosixPath(n).parts}
                if len(prefixes) != 1:
                    errors.append("multiple_prefixes")
                for name in names:
                    p = PurePosixPath(name)
                    if p.is_absolute() or ".." in p.parts:
                        errors.append(f"unsafe:{name}")
                    mode = (zf.getinfo(name).external_attr >> 16) & 0o170000
                    if mode == stat.S_IFLNK:
                        errors.append(f"link:{name}")
                prefix = next(iter(prefixes), "")
                catalog_name = f"{prefix}/EVIDENCE_CATALOG.json"
                if catalog_name not in names:
                    errors.append("catalog_missing")
                    catalog = {}
                else:
                    catalog = json.loads(zf.read(catalog_name).decode("utf-8"))
                for entry in catalog.get("entries", []):
                    member = f"{prefix}/evidence/{entry['path']}"
                    if member not in names:
                        errors.append(f"missing:{entry['path']}")
                    elif sha256_bytes(zf.read(member)) != entry["sha256"]:
                        errors.append(f"changed:{entry['path']}")
    except (OSError, zipfile.BadZipFile, json.JSONDecodeError) as exc:
        errors.append(str(exc))
        catalog = {}
    checks.append(check("bundle_integrity", not errors, errors))
    checks.append(check("catalog_passed", catalog.get("passed") is True))
    passed = all(c["passed"] for c in checks)
    return result("certification_bundle", passed, checks, archive_sha256=sha256_file(archive), catalog_digest=catalog.get("catalog_digest"))


def final_gate(artifacts: dict[str, Any], required_kinds: Sequence[str]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    supplied: dict[str, dict[str, Any]] = {}
    for path_text in artifacts.get("artifact_paths", []):
        path = Path(path_text)
        doc = read_json(ensure_regular(path, label="gate artifact"))
        kind = str(doc.get("kind", ""))
        if kind in supplied:
            raise OperationsError(f"duplicate gate artifact kind: {kind}")
        supplied[kind] = doc
    missing = sorted(set(required_kinds) - set(supplied))
    failed = sorted(kind for kind, doc in supplied.items() if kind in required_kinds and doc.get("passed") is not True)
    checks.append(check("required_artifacts_present", not missing, missing))
    checks.append(check("required_artifacts_passed", not failed, failed))
    digests = {kind: sha256_bytes(canonical_json_bytes(supplied[kind])) for kind in sorted(set(required_kinds) & set(supplied))}
    checks.append(check("artifact_set_complete", len(digests) == len(set(required_kinds))))
    passed = all(c["passed"] for c in checks)
    return result(
        "local_production_gate",
        passed,
        checks,
        scope="local-operational-assurance-only",
        external_certification_required=True,
        artifact_digests=digests,
        gate_digest=sha256_bytes(canonical_json_bytes(digests)),
    )


def emit(doc: dict[str, Any], output: Path | None) -> int:
    if output:
        write_json(output, doc)
    else:
        sys.stdout.buffer.write(canonical_json_bytes(doc))
    return 0 if doc.get("passed") is True else 1


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    def json_pair(name: str, second: str | None = None) -> argparse.ArgumentParser:
        p = sub.add_parser(name)
        p.add_argument("--input", type=Path, required=True)
        if second:
            p.add_argument(f"--{second}", type=Path, required=True)
        p.add_argument("--output", type=Path)
        return p

    json_pair("deployment-validate")
    json_pair("environment-parity", "policy")
    json_pair("compatibility-validate", "policy")
    json_pair("upgrade-validate")
    json_pair("rollback-drill")
    json_pair("capacity-evaluate", "policy")
    json_pair("soak-evaluate", "policy")
    json_pair("observability-validate", "policy")
    json_pair("alert-routing-validate", "policy")
    json_pair("data-governance-validate")
    json_pair("incident-tabletop-evaluate", "policy")
    json_pair("approvals-validate", "policy")

    p = sub.add_parser("evidence-catalog")
    p.add_argument("--root", type=Path, required=True)
    p.add_argument("--specification", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("bundle-build")
    p.add_argument("--root", type=Path, required=True)
    p.add_argument("--catalog", type=Path, required=True)
    p.add_argument("--archive", type=Path, required=True)
    p.add_argument("--prefix", required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("bundle-verify")
    p.add_argument("--archive", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("final-gate")
    p.add_argument("--artifacts", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--output", type=Path)

    args = parser.parse_args(argv)
    try:
        command = args.command
        if command == "deployment-validate":
            doc = validate_deployment_contract(read_json(args.input))
        elif command == "environment-parity":
            doc = environment_parity(read_json(args.input), read_json(args.policy))
        elif command == "compatibility-validate":
            doc = compatibility_validate(read_json(args.input), read_json(args.policy))
        elif command == "upgrade-validate":
            doc = upgrade_plan_validate(read_json(args.input))
        elif command == "rollback-drill":
            doc = rollback_drill(read_json(args.input))
        elif command == "capacity-evaluate":
            doc = capacity_evaluate(read_json(args.input), read_json(args.policy))
        elif command == "soak-evaluate":
            doc = soak_evaluate(read_json(args.input), read_json(args.policy))
        elif command == "observability-validate":
            doc = observability_validate(read_json(args.input), read_json(args.policy))
        elif command == "alert-routing-validate":
            doc = alert_routing_validate(read_json(args.input), read_json(args.policy))
        elif command == "data-governance-validate":
            doc = data_governance_validate(read_json(args.input))
        elif command == "incident-tabletop-evaluate":
            doc = incident_tabletop_evaluate(read_json(args.input), read_json(args.policy))
        elif command == "approvals-validate":
            doc = approvals_validate(read_json(args.input), read_json(args.policy))
        elif command == "evidence-catalog":
            doc = evidence_catalog(args.root, read_json(args.specification))
        elif command == "bundle-build":
            doc = certification_bundle(args.root, read_json(args.catalog), args.archive, args.prefix)
        elif command == "bundle-verify":
            doc = verify_certification_bundle(args.archive)
        elif command == "final-gate":
            artifact_spec = read_json(args.artifacts)
            policy = read_json(args.policy)
            doc = final_gate(artifact_spec, policy.get("required_artifact_kinds", []))
        else:
            raise OperationsError(f"unsupported command: {command}")
        return emit(doc, getattr(args, "output", None))
    except OperationsError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
