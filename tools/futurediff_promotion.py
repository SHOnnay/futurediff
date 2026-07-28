#!/usr/bin/env python3
"""FutureDiff external-evidence and release-promotion assurance toolkit.

The toolkit is deliberately fail-closed. It validates externally produced
certification evidence, hosted workflow identity, risk exceptions,
post-deployment health, rollback readiness, and append-only transparency
records before allowing a production-promotion decision.
"""
from __future__ import annotations

import argparse
import datetime as dt
import fnmatch
import hashlib
import json
import os
import stat
import sys
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any, Iterable, Sequence

FORMAT_VERSION = "1.0"
ZERO_HASH = "0" * 64
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)


class PromotionError(RuntimeError):
    pass


def canonical_json_bytes(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode("utf-8")


def read_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise PromotionError(f"cannot read JSON {path}: {exc}") from exc


def write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    tmp.write_bytes(canonical_json_bytes(value))
    os.replace(tmp, path)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def is_sha256(value: Any) -> bool:
    return isinstance(value, str) and len(value) == 64 and all(c in "0123456789abcdef" for c in value.lower())


def ensure_regular(path: Path, *, label: str = "file") -> Path:
    try:
        st = path.lstat()
    except OSError as exc:
        raise PromotionError(f"{label} unavailable: {path}: {exc}") from exc
    if stat.S_ISLNK(st.st_mode):
        raise PromotionError(f"{label} symbolic link rejected: {path}")
    if not stat.S_ISREG(st.st_mode):
        raise PromotionError(f"{label} is not a regular file: {path}")
    return path


def safe_relative(value: str) -> bool:
    p = PurePosixPath(value)
    return bool(value) and not p.is_absolute() and ".." not in p.parts and "" not in p.parts


def parse_time(value: Any) -> dt.datetime:
    if isinstance(value, (int, float)):
        parsed = dt.datetime.fromtimestamp(float(value), tz=dt.timezone.utc)
    elif isinstance(value, str):
        text = value.strip()
        if text.endswith("Z"):
            text = text[:-1] + "+00:00"
        try:
            parsed = dt.datetime.fromisoformat(text)
        except ValueError as exc:
            raise PromotionError(f"invalid timestamp: {value}") from exc
        if parsed.tzinfo is None:
            raise PromotionError(f"timestamp must include a timezone: {value}")
        parsed = parsed.astimezone(dt.timezone.utc)
    else:
        raise PromotionError(f"invalid timestamp value: {value!r}")
    return parsed


def iso_time(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def utc_now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def check(name: str, passed: bool, detail: Any = None) -> dict[str, Any]:
    item: dict[str, Any] = {"name": name, "passed": bool(passed)}
    if detail is not None:
        item["detail"] = detail
    return item


def result(kind: str, passed: bool, checks: list[dict[str, Any]], **extra: Any) -> dict[str, Any]:
    material = {
        "format_version": FORMAT_VERSION,
        "kind": kind,
        "passed": bool(passed),
        "checks": checks,
        **extra,
    }
    material["result_digest"] = sha256_bytes(canonical_json_bytes(material))
    return material


def _time_or_none(value: Any) -> dt.datetime | None:
    if value in (None, ""):
        return None
    return parse_time(value)


def evidence_intake(root: Path, specification: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = (now or utc_now()).astimezone(dt.timezone.utc)
    root = root.resolve(strict=True)
    items = specification.get("evidence", [])
    if not isinstance(items, list):
        raise PromotionError("evidence specification must contain an evidence list")

    checks: list[dict[str, Any]] = []
    entries: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    duplicate_ids: list[str] = []
    missing_files: list[str] = []
    invalid_files: list[str] = []
    digest_failures: list[str] = []
    producer_failures: list[str] = []
    source_failures: list[str] = []
    environment_failures: list[str] = []
    time_failures: list[str] = []
    synthetic_failures: list[str] = []

    required_types = set(str(x) for x in policy.get("required_types", []))
    allowed_producers = set(str(x) for x in policy.get("allowed_producers", []))
    allowed_sources = set(str(x) for x in policy.get("allowed_sources", []))
    required_environment = str(policy.get("environment", "production"))
    require_non_synthetic = bool(policy.get("require_non_synthetic", True))
    default_max_age = float(policy.get("default_max_age_hours", 72))
    max_age_by_type = {str(k): float(v) for k, v in policy.get("max_age_hours_by_type", {}).items()}
    max_future_skew = float(policy.get("max_future_skew_seconds", 300))

    for raw in items:
        if not isinstance(raw, dict):
            invalid_files.append("non-object-entry")
            continue
        evidence_id = str(raw.get("id", "")).strip()
        evidence_type = str(raw.get("type", "")).strip()
        rel = str(raw.get("path", "")).strip()
        if not evidence_id or evidence_id in seen_ids:
            duplicate_ids.append(evidence_id or "<missing>")
        seen_ids.add(evidence_id)
        if not safe_relative(rel):
            invalid_files.append(rel or evidence_id)
            continue
        path = root / rel
        try:
            ensure_regular(path, label="external evidence")
        except PromotionError:
            if not path.exists():
                missing_files.append(rel)
            else:
                invalid_files.append(rel)
            continue

        actual_digest = sha256_file(path)
        declared_digest = str(raw.get("sha256", "")).lower()
        if not is_sha256(declared_digest) or actual_digest != declared_digest:
            digest_failures.append(evidence_id or rel)

        producer = str(raw.get("producer", "")).strip()
        source = str(raw.get("source", "")).strip()
        environment = str(raw.get("environment", "")).strip()
        if allowed_producers and producer not in allowed_producers:
            producer_failures.append(evidence_id or rel)
        if allowed_sources and source not in allowed_sources:
            source_failures.append(evidence_id or rel)
        if environment != required_environment:
            environment_failures.append(evidence_id or rel)

        try:
            issued_at = parse_time(raw.get("issued_at"))
            expires_at = _time_or_none(raw.get("expires_at"))
            max_age = max_age_by_type.get(evidence_type, default_max_age)
            age_hours = (now - issued_at).total_seconds() / 3600
            if issued_at > now + dt.timedelta(seconds=max_future_skew):
                time_failures.append(f"{evidence_id}:future")
            if age_hours > max_age:
                time_failures.append(f"{evidence_id}:stale")
            if expires_at is not None and expires_at <= now:
                time_failures.append(f"{evidence_id}:expired")
            if expires_at is not None and expires_at <= issued_at:
                time_failures.append(f"{evidence_id}:invalid-window")
        except PromotionError:
            issued_at = now
            expires_at = None
            time_failures.append(f"{evidence_id}:invalid-time")

        synthetic = bool(raw.get("synthetic", False))
        if require_non_synthetic and synthetic:
            synthetic_failures.append(evidence_id or rel)

        entries.append({
            "id": evidence_id,
            "type": evidence_type,
            "path": rel,
            "sha256": actual_digest,
            "size": path.stat().st_size,
            "producer": producer,
            "source": source,
            "environment": environment,
            "issued_at": iso_time(issued_at),
            "expires_at": iso_time(expires_at) if expires_at else None,
            "synthetic": synthetic,
        })

    present_types = {entry["type"] for entry in entries}
    missing_types = sorted(required_types - present_types)
    entries.sort(key=lambda x: (x["type"], x["id"], x["path"]))
    checks.extend([
        check("unique_evidence_ids", not duplicate_ids, sorted(duplicate_ids)),
        check("required_evidence_types_present", not missing_types, missing_types),
        check("evidence_files_present", not missing_files, sorted(missing_files)),
        check("safe_regular_evidence_files", not invalid_files, sorted(invalid_files)),
        check("declared_digests_match", not digest_failures, sorted(digest_failures)),
        check("allowed_producers", not producer_failures, sorted(producer_failures)),
        check("allowed_sources", not source_failures, sorted(source_failures)),
        check("production_environment_only", not environment_failures, sorted(environment_failures)),
        check("fresh_unexpired_evidence", not time_failures, sorted(time_failures)),
        check("non_synthetic_evidence", not synthetic_failures, sorted(synthetic_failures)),
    ])
    digest_material = [{k: entry[k] for k in ("id", "type", "sha256", "producer", "source", "environment", "issued_at", "expires_at", "synthetic")} for entry in entries]
    passed = all(item["passed"] for item in checks)
    return result(
        "external_evidence_intake",
        passed,
        checks,
        evaluated_at=iso_time(now),
        environment=required_environment,
        entries=entries,
        evidence_set_digest=sha256_bytes(canonical_json_bytes(digest_material)),
        external_certification=True,
    )


def _claim_time(claims: dict[str, Any], key: str) -> dt.datetime:
    if key not in claims:
        raise PromotionError(f"missing OIDC claim: {key}")
    return parse_time(claims[key])


def oidc_claims_verify(claims: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = (now or utc_now()).astimezone(dt.timezone.utc)
    checks: list[dict[str, Any]] = []

    issuer = str(claims.get("iss", ""))
    audience = claims.get("aud")
    audiences = {str(audience)} if isinstance(audience, str) else {str(x) for x in audience or []}
    repository = str(claims.get("repository", ""))
    workflow_ref = str(claims.get("workflow_ref", ""))
    git_ref = str(claims.get("ref", ""))
    event_name = str(claims.get("event_name", ""))
    sha = str(claims.get("sha", "")).lower()
    run_id = str(claims.get("run_id", ""))
    actor = str(claims.get("actor", ""))

    allowed_audiences = set(str(x) for x in policy.get("allowed_audiences", []))
    allowed_workflows = [str(x) for x in policy.get("allowed_workflow_refs", [])]
    allowed_refs = [str(x) for x in policy.get("allowed_refs", [])]
    allowed_events = set(str(x) for x in policy.get("allowed_events", []))
    denied_actors = set(str(x) for x in policy.get("denied_actors", []))

    checks.extend([
        check("trusted_issuer", issuer == str(policy.get("issuer", "")), issuer),
        check("trusted_audience", bool(audiences & allowed_audiences), sorted(audiences)),
        check("expected_repository", repository == str(policy.get("repository", "")), repository),
        check("allowed_workflow", any(fnmatch.fnmatchcase(workflow_ref, pattern) for pattern in allowed_workflows), workflow_ref),
        check("allowed_ref", any(fnmatch.fnmatchcase(git_ref, pattern) for pattern in allowed_refs), git_ref),
        check("allowed_event", not allowed_events or event_name in allowed_events, event_name),
        check("protected_ref", claims.get("ref_protected") is True),
        check("valid_source_sha", len(sha) in (40, 64) and all(c in "0123456789abcdef" for c in sha), sha),
        check("valid_run_id", run_id.isdigit() and int(run_id) > 0, run_id),
        check("allowed_actor", bool(actor) and actor not in denied_actors, actor),
    ])

    time_errors: list[str] = []
    try:
        issued = _claim_time(claims, "iat")
        expires = _claim_time(claims, "exp")
        max_age = float(policy.get("maximum_token_age_seconds", 900))
        future_skew = float(policy.get("max_future_skew_seconds", 60))
        if issued > now + dt.timedelta(seconds=future_skew):
            time_errors.append("issued_in_future")
        if (now - issued).total_seconds() > max_age:
            time_errors.append("too_old")
        if expires <= now:
            time_errors.append("expired")
        if expires <= issued:
            time_errors.append("invalid_window")
    except PromotionError as exc:
        issued = now
        expires = now
        time_errors.append(str(exc))
    checks.append(check("fresh_valid_identity_window", not time_errors, time_errors))

    identity_material = {
        "issuer": issuer,
        "audiences": sorted(audiences),
        "repository": repository,
        "workflow_ref": workflow_ref,
        "ref": git_ref,
        "event_name": event_name,
        "sha": sha,
        "run_id": run_id,
        "actor": actor,
        "issued_at": iso_time(issued),
        "expires_at": iso_time(expires),
    }
    passed = all(item["passed"] for item in checks)
    return result("hosted_workflow_identity", passed, checks, identity=identity_material, identity_digest=sha256_bytes(canonical_json_bytes(identity_material)))


def exception_validate(record: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = (now or utc_now()).astimezone(dt.timezone.utc)
    checks: list[dict[str, Any]] = []
    exception_id = str(record.get("id", "")).strip()
    owner = str(record.get("owner", "")).strip()
    scope = str(record.get("scope", "")).strip()
    risk = str(record.get("risk", "")).strip()
    approvers = record.get("approvals", [])
    controls = record.get("compensating_controls", [])
    allowed_scopes = set(str(x) for x in policy.get("allowed_scopes", []))
    allowed_risks = set(str(x) for x in policy.get("allowed_risks", []))
    required_roles = set(str(x) for x in policy.get("required_roles", []))
    max_duration = float(policy.get("maximum_duration_hours", 24))

    checks.extend([
        check("exception_id_present", bool(exception_id)),
        check("owner_present", bool(owner)),
        check("scope_allowed", scope in allowed_scopes, scope),
        check("risk_allowed", risk in allowed_risks, risk),
        check("rationale_present", len(str(record.get("rationale", "")).strip()) >= int(policy.get("minimum_rationale_length", 20))),
        check("compensating_controls_present", isinstance(controls, list) and len(controls) >= int(policy.get("minimum_compensating_controls", 1))),
    ])

    time_errors: list[str] = []
    try:
        created = parse_time(record.get("created_at"))
        expires = parse_time(record.get("expires_at"))
        if created > now:
            time_errors.append("created_in_future")
        if expires <= now:
            time_errors.append("expired")
        if expires <= created:
            time_errors.append("invalid_window")
        if (expires - created).total_seconds() > max_duration * 3600:
            time_errors.append("duration_exceeds_policy")
    except PromotionError as exc:
        created = now
        expires = now
        time_errors.append(str(exc))
    checks.append(check("temporary_valid_window", not time_errors, time_errors))

    actors: set[str] = set()
    roles: set[str] = set()
    invalid_approvals: list[str] = []
    if not isinstance(approvers, list):
        approvers = []
    for approval in approvers:
        actor = str(approval.get("actor", "")).strip() if isinstance(approval, dict) else ""
        role = str(approval.get("role", "")).strip() if isinstance(approval, dict) else ""
        decision = str(approval.get("decision", "")).strip() if isinstance(approval, dict) else ""
        if not actor or actor in actors or decision != "approved":
            invalid_approvals.append(actor or "<missing>")
        actors.add(actor)
        roles.add(role)
    checks.extend([
        check("unique_valid_approvals", not invalid_approvals, invalid_approvals),
        check("required_approval_roles", not (required_roles - roles), sorted(required_roles - roles)),
        check("no_self_approval", not bool(policy.get("disallow_owner_approval", True)) or owner not in actors, owner),
    ])
    passed = all(item["passed"] for item in checks)
    material = {
        "id": exception_id,
        "owner": owner,
        "scope": scope,
        "risk": risk,
        "created_at": iso_time(created),
        "expires_at": iso_time(expires),
        "approver_roles": sorted(roles),
        "compensating_controls": controls,
    }
    return result("risk_exception", passed, checks, exception=material, exception_digest=sha256_bytes(canonical_json_bytes(material)))


def transparency_append(ledger: dict[str, Any], record: dict[str, Any]) -> dict[str, Any]:
    entries = list(ledger.get("entries", []))
    if ledger and ledger.get("format_version") not in (None, FORMAT_VERSION):
        raise PromotionError("unsupported transparency ledger format")
    verification = transparency_verify({"format_version": FORMAT_VERSION, "entries": entries})
    if entries and not verification["passed"]:
        raise PromotionError("cannot append to an invalid transparency ledger")
    recorded_at = parse_time(record.get("recorded_at"))
    payload = record.get("payload")
    if not isinstance(payload, dict):
        raise PromotionError("transparency record payload must be an object")
    record_digest = sha256_bytes(canonical_json_bytes(payload))
    if any(entry.get("record_digest") == record_digest for entry in entries):
        raise PromotionError("duplicate transparency record digest")
    sequence = len(entries) + 1
    previous_hash = entries[-1]["entry_hash"] if entries else ZERO_HASH
    material = {
        "sequence": sequence,
        "previous_hash": previous_hash,
        "recorded_at": iso_time(recorded_at),
        "record_digest": record_digest,
    }
    entry = {**material, "entry_hash": sha256_bytes(canonical_json_bytes(material)), "payload": payload}
    entries.append(entry)
    updated = {"format_version": FORMAT_VERSION, "entries": entries, "head": entry["entry_hash"], "entry_count": len(entries)}
    updated["ledger_digest"] = sha256_bytes(canonical_json_bytes({"entries": entries, "head": updated["head"]}))
    return updated


def transparency_verify(ledger: dict[str, Any]) -> dict[str, Any]:
    entries = ledger.get("entries", [])
    checks: list[dict[str, Any]] = []
    errors: list[str] = []
    previous = ZERO_HASH
    if not isinstance(entries, list):
        entries = []
        errors.append("entries_not_list")
    for index, entry in enumerate(entries, start=1):
        if not isinstance(entry, dict):
            errors.append(f"entry_{index}_not_object")
            continue
        payload = entry.get("payload")
        if not isinstance(payload, dict):
            errors.append(f"entry_{index}_payload")
            continue
        expected_record_digest = sha256_bytes(canonical_json_bytes(payload))
        material = {
            "sequence": index,
            "previous_hash": previous,
            "recorded_at": str(entry.get("recorded_at", "")),
            "record_digest": expected_record_digest,
        }
        expected_hash = sha256_bytes(canonical_json_bytes(material))
        if entry.get("sequence") != index:
            errors.append(f"entry_{index}_sequence")
        if entry.get("previous_hash") != previous:
            errors.append(f"entry_{index}_previous")
        if entry.get("record_digest") != expected_record_digest:
            errors.append(f"entry_{index}_record_digest")
        if entry.get("entry_hash") != expected_hash:
            errors.append(f"entry_{index}_entry_hash")
        try:
            parse_time(entry.get("recorded_at"))
        except PromotionError:
            errors.append(f"entry_{index}_recorded_at")
        previous = expected_hash
    declared_head = ledger.get("head", previous if entries else ZERO_HASH)
    if declared_head != (entries[-1].get("entry_hash") if entries else ZERO_HASH):
        errors.append("head_mismatch")
    checks.append(check("hash_chain_integrity", not errors, errors))
    return result("transparency_ledger", not errors, checks, entry_count=len(entries), head=declared_head)


def _load_exception_docs(paths: Iterable[Path]) -> list[dict[str, Any]]:
    return [read_json(ensure_regular(path, label="risk exception result")) for path in paths]


def promotion_evaluate(candidate: dict[str, Any], intake: dict[str, Any], identity: dict[str, Any], approvals: dict[str, Any], policy: dict[str, Any], exceptions: Sequence[dict[str, Any]] = ()) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    archive_digest = str(candidate.get("archive_sha256", "")).lower()
    candidate_version = str(candidate.get("version", "")).strip()
    release_digest = str(approvals.get("release_digest", "")).lower()
    approved_roles = {str(item.get("role", "")) for item in approvals.get("approvals", []) if isinstance(item, dict)}
    required_roles = set(str(x) for x in policy.get("required_approval_roles", []))

    checks.extend([
        check("release_candidate_approved", candidate.get("approved") is True),
        check("release_candidate_digest", is_sha256(archive_digest), archive_digest),
        check("external_evidence_passed", intake.get("passed") is True and intake.get("external_certification") is True),
        check("external_evidence_non_synthetic", not any(bool(entry.get("synthetic")) for entry in intake.get("entries", []))),
        check("hosted_identity_passed", identity.get("passed") is True),
        check("release_approvals_passed", approvals.get("passed") is True or approvals.get("approved") is True),
        check("approval_digest_bound", release_digest == archive_digest, {"approval": release_digest, "archive": archive_digest}),
        check("required_approval_roles", not (required_roles - approved_roles), sorted(required_roles - approved_roles)),
        check("version_allowed", bool(candidate_version) and not candidate_version.endswith(("-dev", "-dirty")), candidate_version),
    ])

    allowed_exception_scopes = set(str(x) for x in policy.get("allowed_exception_scopes", []))
    invalid_exceptions: list[str] = []
    exception_digests: list[str] = []
    for doc in exceptions:
        scope = str(doc.get("exception", {}).get("scope", ""))
        if doc.get("passed") is not True or scope not in allowed_exception_scopes:
            invalid_exceptions.append(str(doc.get("exception", {}).get("id", "<unknown>")))
        digest = doc.get("exception_digest")
        if is_sha256(digest):
            exception_digests.append(str(digest))
    if exceptions and not bool(policy.get("allow_exceptions", False)):
        invalid_exceptions.append("exceptions_not_allowed")
    checks.append(check("risk_exceptions_valid", not invalid_exceptions, sorted(invalid_exceptions)))

    evidence_digest = str(intake.get("evidence_set_digest", ""))
    identity_digest = str(identity.get("identity_digest", ""))
    binding = {
        "version": candidate_version,
        "archive_sha256": archive_digest,
        "external_evidence_digest": evidence_digest,
        "hosted_identity_digest": identity_digest,
        "exception_digests": sorted(exception_digests),
    }
    passed = all(item["passed"] for item in checks)
    return result(
        "production_promotion_decision",
        passed,
        checks,
        approved=passed,
        scope="external-production-promotion",
        version=candidate_version,
        archive_sha256=archive_digest,
        promotion_binding=binding,
        promotion_digest=sha256_bytes(canonical_json_bytes(binding)),
    )


def postdeploy_evaluate(evidence: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    required_health = set(str(x) for x in policy.get("required_health_checks", []))
    health = {str(item.get("name", "")): item for item in evidence.get("health_checks", []) if isinstance(item, dict)}
    missing_health = sorted(required_health - set(health))
    failed_health = sorted(name for name in required_health & set(health) if health[name].get("passed") is not True)
    checks.extend([
        check("deployment_digest_valid", is_sha256(str(evidence.get("deployment_digest", "")))),
        check("non_synthetic_observation", evidence.get("synthetic") is False),
        check("minimum_observation_window", float(evidence.get("duration_seconds", 0)) >= float(policy.get("minimum_duration_seconds", 900))),
        check("required_health_checks_present", not missing_health, missing_health),
        check("required_health_checks_passed", not failed_health, failed_health),
        check("availability", float(evidence.get("availability", 0)) >= float(policy.get("minimum_availability", 0.999))),
        check("error_rate", float(evidence.get("error_rate", 1)) <= float(policy.get("maximum_error_rate", 0.001))),
        check("p95_latency", float(evidence.get("p95_latency_ms", float("inf"))) <= float(policy.get("maximum_p95_latency_ms", 500))),
        check("unknown_outcomes", int(evidence.get("unknown_outcomes", 1)) <= int(policy.get("maximum_unknown_outcomes", 0))),
        check("effect_reconciliation", int(evidence.get("unreconciled_effects", 1)) <= int(policy.get("maximum_unreconciled_effects", 0))),
    ])
    passed = all(item["passed"] for item in checks)
    metrics = {key: evidence.get(key) for key in ("duration_seconds", "availability", "error_rate", "p95_latency_ms", "unknown_outcomes", "unreconciled_effects")}
    return result("post_deployment_health", passed, checks, deployment_digest=evidence.get("deployment_digest"), metrics=metrics)


def rollback_evaluate(evidence: dict[str, Any], policy: dict[str, Any], now: dt.datetime | None = None) -> dict[str, Any]:
    now = (now or utc_now()).astimezone(dt.timezone.utc)
    checks: list[dict[str, Any]] = []
    trigger_reasons: list[str] = []
    metrics = evidence.get("current_metrics", {})
    if float(metrics.get("error_rate", 0)) > float(policy.get("trigger_error_rate", 0.02)):
        trigger_reasons.append("error_rate")
    if float(metrics.get("p95_latency_ms", 0)) > float(policy.get("trigger_p95_latency_ms", 2000)):
        trigger_reasons.append("p95_latency")
    if int(metrics.get("unknown_outcomes", 0)) > int(policy.get("trigger_unknown_outcomes", 0)):
        trigger_reasons.append("unknown_outcomes")
    if int(metrics.get("unreconciled_effects", 0)) > int(policy.get("trigger_unreconciled_effects", 0)):
        trigger_reasons.append("unreconciled_effects")

    drill_errors: list[str] = []
    try:
        last_drill = parse_time(evidence.get("last_drill_at"))
        age_days = (now - last_drill).total_seconds() / 86400
        if age_days > float(policy.get("maximum_drill_age_days", 90)):
            drill_errors.append("drill_stale")
        if last_drill > now:
            drill_errors.append("drill_in_future")
    except PromotionError as exc:
        last_drill = now
        drill_errors.append(str(exc))

    checks.extend([
        check("rollback_plan_digest", is_sha256(str(evidence.get("rollback_plan_digest", "")))),
        check("verified_backup_digest", is_sha256(str(evidence.get("backup_digest", ""))) and evidence.get("backup_verified") is True),
        check("non_synthetic_drill", evidence.get("synthetic") is False),
        check("recent_successful_drill", not drill_errors and evidence.get("last_drill_passed") is True, drill_errors),
        check("rto_within_policy", float(evidence.get("tested_rto_seconds", float("inf"))) <= float(policy.get("maximum_rto_seconds", 600))),
        check("rpo_within_policy", float(evidence.get("tested_rpo_seconds", float("inf"))) <= float(policy.get("maximum_rpo_seconds", 300))),
        check("automatic_trigger_evaluation", isinstance(metrics, dict)),
    ])
    passed = all(item["passed"] for item in checks)
    decision = "rollback" if trigger_reasons else "continue"
    return result(
        "rollback_decision",
        passed,
        checks,
        decision=decision,
        trigger_reasons=trigger_reasons,
        rollback_ready=passed,
        last_drill_at=iso_time(last_drill),
    )


def launch_checklist(promotion: dict[str, Any], postdeploy: dict[str, Any], rollback: dict[str, Any], policy: dict[str, Any]) -> dict[str, Any]:
    checks = [
        check("promotion_approved", promotion.get("approved") is True and promotion.get("passed") is True),
        check("post_deployment_health_passed", postdeploy.get("passed") is True),
        check("rollback_ready", rollback.get("passed") is True and rollback.get("rollback_ready") is True),
        check("rollback_not_required", rollback.get("decision") == "continue", rollback.get("trigger_reasons", [])),
        check("runbook_acknowledged", policy.get("runbook_acknowledged") is True),
        check("on_call_confirmed", policy.get("on_call_confirmed") is True),
        check("communications_ready", policy.get("communications_ready") is True),
    ]
    passed = all(item["passed"] for item in checks)
    binding = {
        "promotion_digest": promotion.get("promotion_digest"),
        "postdeploy_digest": postdeploy.get("result_digest"),
        "rollback_digest": rollback.get("result_digest"),
    }
    return result(
        "production_launch_checklist",
        passed,
        checks,
        production_complete=passed,
        scope="externally-certified-production-launch",
        launch_binding=binding,
        launch_digest=sha256_bytes(canonical_json_bytes(binding)),
    )


def release_metadata(candidate: dict[str, Any], promotion: dict[str, Any], ledger: dict[str, Any]) -> dict[str, Any]:
    verification = transparency_verify(ledger)
    checks = [
        check("candidate_approved", candidate.get("approved") is True),
        check("promotion_approved", promotion.get("approved") is True),
        check("transparency_ledger_valid", verification.get("passed") is True),
        check("candidate_digest_matches_promotion", candidate.get("archive_sha256") == promotion.get("archive_sha256")),
    ]
    passed = all(item["passed"] for item in checks)
    version = str(candidate.get("version", ""))
    archive = str(candidate.get("archive", ""))
    body_lines = [
        f"FutureDiff {version}",
        "",
        "Release promotion has passed external evidence, hosted identity, approval, and operational checks.",
        "",
        f"Source archive SHA-256: `{candidate.get('archive_sha256', '')}`",
        f"Promotion digest: `{promotion.get('promotion_digest', '')}`",
        f"Transparency head: `{ledger.get('head', '')}`",
    ]
    metadata = {
        "tag_name": version,
        "name": f"FutureDiff {version}",
        "draft": False,
        "prerelease": False,
        "body": "\n".join(body_lines),
        "assets": [{"path": archive, "sha256": candidate.get("archive_sha256")}],
        "promotion_digest": promotion.get("promotion_digest"),
        "transparency_head": ledger.get("head"),
    }
    return result("github_release_metadata", passed, checks, metadata=metadata, metadata_digest=sha256_bytes(canonical_json_bytes(metadata)))


def promotion_bundle(root: Path, specification: dict[str, Any], output: Path, prefix: str) -> dict[str, Any]:
    root = root.resolve(strict=True)
    if not safe_relative(prefix):
        raise PromotionError("unsafe promotion bundle prefix")
    entries: list[dict[str, Any]] = []
    seen: set[str] = set()
    for raw in specification.get("artifacts", []):
        rel = str(raw.get("path", ""))
        artifact_id = str(raw.get("id", rel))
        if artifact_id in seen:
            raise PromotionError(f"duplicate promotion artifact id: {artifact_id}")
        seen.add(artifact_id)
        if not safe_relative(rel):
            raise PromotionError(f"unsafe promotion artifact path: {rel}")
        path = ensure_regular(root / rel, label="promotion artifact")
        entries.append({"id": artifact_id, "path": rel, "sha256": sha256_file(path), "size": path.stat().st_size})
    entries.sort(key=lambda x: (x["id"], x["path"]))
    catalog = {"format_version": FORMAT_VERSION, "entries": entries}
    catalog["catalog_digest"] = sha256_bytes(canonical_json_bytes(catalog))
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9, strict_timestamps=True) as archive:
        info = zipfile.ZipInfo(f"{prefix}/PROMOTION_CATALOG.json", FIXED_ZIP_TIME)
        info.external_attr = 0o100644 << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        archive.writestr(info, canonical_json_bytes(catalog))
        for entry in entries:
            path = ensure_regular(root / entry["path"], label="promotion artifact")
            if sha256_file(path) != entry["sha256"]:
                raise PromotionError(f"promotion artifact changed while bundling: {entry['path']}")
            info = zipfile.ZipInfo(f"{prefix}/artifacts/{entry['path']}", FIXED_ZIP_TIME)
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, path.read_bytes())
    return promotion_bundle_verify(output)


def promotion_bundle_verify(archive_path: Path) -> dict[str, Any]:
    ensure_regular(archive_path, label="promotion bundle")
    errors: list[str] = []
    catalog: dict[str, Any] = {}
    try:
        with zipfile.ZipFile(archive_path, "r") as archive:
            names = archive.namelist()
            prefixes = {PurePosixPath(name).parts[0] for name in names if PurePosixPath(name).parts}
            if len(prefixes) != 1:
                errors.append("invalid_prefix_count")
            for name in names:
                path = PurePosixPath(name)
                if path.is_absolute() or ".." in path.parts:
                    errors.append(f"unsafe_path:{name}")
                mode = (archive.getinfo(name).external_attr >> 16) & 0o170000
                if mode == stat.S_IFLNK:
                    errors.append(f"symlink:{name}")
            prefix = next(iter(prefixes), "")
            catalog_name = f"{prefix}/PROMOTION_CATALOG.json"
            if catalog_name not in names:
                errors.append("catalog_missing")
            else:
                catalog = json.loads(archive.read(catalog_name).decode("utf-8"))
                material = {"format_version": catalog.get("format_version"), "entries": catalog.get("entries", [])}
                if catalog.get("catalog_digest") != sha256_bytes(canonical_json_bytes(material)):
                    errors.append("catalog_digest")
                for entry in catalog.get("entries", []):
                    member = f"{prefix}/artifacts/{entry.get('path', '')}"
                    if member not in names:
                        errors.append(f"missing:{entry.get('path')}")
                    else:
                        data = archive.read(member)
                        if len(data) != int(entry.get("size", -1)) or sha256_bytes(data) != entry.get("sha256"):
                            errors.append(f"changed:{entry.get('path')}")
    except (OSError, zipfile.BadZipFile, json.JSONDecodeError) as exc:
        errors.append(str(exc))
    checks = [check("promotion_bundle_integrity", not errors, errors)]
    return result("production_promotion_bundle", not errors, checks, archive_sha256=sha256_file(archive_path), catalog_digest=catalog.get("catalog_digest"))


def emit(document: dict[str, Any], output: Path | None) -> int:
    if output:
        write_json(output, document)
    else:
        sys.stdout.buffer.write(canonical_json_bytes(document))
    return 0 if document.get("passed") is True or document.get("approved") is True else 2


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("evidence-intake")
    p.add_argument("--root", type=Path, required=True)
    p.add_argument("--specification", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--now")
    p.add_argument("--output", type=Path)

    p = sub.add_parser("oidc-claims-verify")
    p.add_argument("--claims", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--now")
    p.add_argument("--output", type=Path)

    p = sub.add_parser("exception-validate")
    p.add_argument("--input", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--now")
    p.add_argument("--output", type=Path)

    p = sub.add_parser("transparency-append")
    p.add_argument("--ledger", type=Path, required=True)
    p.add_argument("--record", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)

    p = sub.add_parser("transparency-verify")
    p.add_argument("--ledger", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("promotion-evaluate")
    p.add_argument("--candidate", type=Path, required=True)
    p.add_argument("--intake", type=Path, required=True)
    p.add_argument("--identity", type=Path, required=True)
    p.add_argument("--approvals", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--exception", type=Path, action="append", default=[])
    p.add_argument("--output", type=Path)

    p = sub.add_parser("postdeploy-evaluate")
    p.add_argument("--input", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("rollback-evaluate")
    p.add_argument("--input", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--now")
    p.add_argument("--output", type=Path)

    p = sub.add_parser("launch-checklist")
    p.add_argument("--promotion", type=Path, required=True)
    p.add_argument("--postdeploy", type=Path, required=True)
    p.add_argument("--rollback", type=Path, required=True)
    p.add_argument("--policy", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("release-metadata")
    p.add_argument("--candidate", type=Path, required=True)
    p.add_argument("--promotion", type=Path, required=True)
    p.add_argument("--ledger", type=Path, required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("bundle-build")
    p.add_argument("--root", type=Path, required=True)
    p.add_argument("--specification", type=Path, required=True)
    p.add_argument("--archive", type=Path, required=True)
    p.add_argument("--prefix", required=True)
    p.add_argument("--output", type=Path)

    p = sub.add_parser("bundle-verify")
    p.add_argument("--archive", type=Path, required=True)
    p.add_argument("--output", type=Path)

    args = parser.parse_args(argv)
    try:
        now = parse_time(args.now) if getattr(args, "now", None) else None
        if args.command == "evidence-intake":
            document = evidence_intake(args.root, read_json(args.specification), read_json(args.policy), now)
        elif args.command == "oidc-claims-verify":
            document = oidc_claims_verify(read_json(args.claims), read_json(args.policy), now)
        elif args.command == "exception-validate":
            document = exception_validate(read_json(args.input), read_json(args.policy), now)
        elif args.command == "transparency-append":
            ledger = read_json(args.ledger) if args.ledger.exists() else {"format_version": FORMAT_VERSION, "entries": []}
            document = transparency_append(ledger, read_json(args.record))
            write_json(args.output, document)
            print(json.dumps(document, sort_keys=True, indent=2))
            return 0
        elif args.command == "transparency-verify":
            document = transparency_verify(read_json(args.ledger))
        elif args.command == "promotion-evaluate":
            document = promotion_evaluate(
                read_json(args.candidate), read_json(args.intake), read_json(args.identity),
                read_json(args.approvals), read_json(args.policy), _load_exception_docs(args.exception),
            )
        elif args.command == "postdeploy-evaluate":
            document = postdeploy_evaluate(read_json(args.input), read_json(args.policy))
        elif args.command == "rollback-evaluate":
            document = rollback_evaluate(read_json(args.input), read_json(args.policy), now)
        elif args.command == "launch-checklist":
            document = launch_checklist(read_json(args.promotion), read_json(args.postdeploy), read_json(args.rollback), read_json(args.policy))
        elif args.command == "release-metadata":
            document = release_metadata(read_json(args.candidate), read_json(args.promotion), read_json(args.ledger))
        elif args.command == "bundle-build":
            document = promotion_bundle(args.root, read_json(args.specification), args.archive, args.prefix)
        elif args.command == "bundle-verify":
            document = promotion_bundle_verify(args.archive)
        else:
            raise PromotionError(f"unsupported command: {args.command}")
        return emit(document, getattr(args, "output", None))
    except (PromotionError, OSError, ValueError, zipfile.BadZipFile) as exc:
        print(json.dumps({"command": args.command, "error": str(exc)}, sort_keys=True), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
