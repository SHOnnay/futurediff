from __future__ import annotations

import importlib.util
import json
import os
import tempfile
import unittest
import zipfile
from pathlib import Path

TOOL = Path(__file__).resolve().parents[1] / "tools" / "futurediff_operations.py"
spec = importlib.util.spec_from_file_location("futurediff_operations", TOOL)
ops = importlib.util.module_from_spec(spec)
assert spec.loader
spec.loader.exec_module(ops)

SHA = "a" * 64


class OperationsTests(unittest.TestCase):
    def contract(self):
        env = {
            "region": "primary",
            "replicas": 2,
            "runtime": "rootless-oci",
            "database": {"engine": "sqlite"},
            "queue": {"engine": "ledger"},
            "secret_provider": "environment",
            "observability": {"metrics": "otel", "logs": "json", "traces": "otel"},
            "backup": {"rpo_seconds": 60, "rto_seconds": 120},
        }
        return {
            "format_version": "1.0",
            "service": {"name": "FutureDiff", "version": "1.0", "owner": "owner@example.invalid"},
            "environments": {"staging": dict(env), "production": dict(env, replicas=3)},
        }

    def test_deployment_contract_passes(self):
        self.assertTrue(ops.validate_deployment_contract(self.contract())["passed"])

    def test_deployment_contract_rejects_secret_value(self):
        doc = self.contract()
        doc["environments"]["production"]["api_token"] = "plaintext"
        self.assertFalse(ops.validate_deployment_contract(doc)["passed"])

    def test_environment_parity(self):
        policy = {"required_equal_fields": ["runtime", "database"], "allowed_different_fields": ["replicas"], "minimum_production_replicas": 2}
        self.assertTrue(ops.environment_parity(self.contract(), policy)["passed"])

    def test_compatibility_requires_evidence(self):
        policy = {"required_combinations": [{"os": "linux", "runtime": "docker", "database": "sqlite"}]}
        good = {"results": [{"os": "linux", "runtime": "docker", "database": "sqlite", "status": "passed", "evidence_sha256": SHA}]}
        self.assertTrue(ops.compatibility_validate(good, policy)["passed"])
        good["results"][0]["evidence_sha256"] = "bad"
        self.assertFalse(ops.compatibility_validate(good, policy)["passed"])

    def upgrade(self):
        return {
            "from_version": "1", "to_version": "2",
            "steps": [
                {"id": "backup", "kind": "backup", "timeout_seconds": 10},
                {"id": "migrate", "kind": "migration", "timeout_seconds": 10, "rollback_step": "rollback-migrate", "backup_required": True},
                {"id": "deploy", "kind": "deploy", "timeout_seconds": 10, "rollback_step": "rollback-deploy", "backup_required": True},
                {"id": "verify", "kind": "verify", "timeout_seconds": 10},
                {"id": "rollback-migrate", "kind": "rollback", "timeout_seconds": 10},
                {"id": "rollback-deploy", "kind": "rollback", "timeout_seconds": 10},
            ],
        }

    def test_upgrade_and_rollback(self):
        self.assertTrue(ops.upgrade_plan_validate(self.upgrade())["passed"])
        self.assertTrue(ops.rollback_drill(self.upgrade())["passed"])

    def test_upgrade_rejects_missing_rollback(self):
        plan = self.upgrade()
        plan["steps"][1].pop("rollback_step")
        self.assertFalse(ops.upgrade_plan_validate(plan)["passed"])

    def test_capacity_thresholds(self):
        policy = {"duration_seconds": 10, "request_count": 100, "concurrency": 2, "throughput_per_second": 5, "p95_latency_ms": 200, "p99_latency_ms": 400, "error_rate": .01, "cpu_peak_percent": 90, "memory_peak_percent": 90, "unknown_outcomes": 0}
        evidence = {"evidence_id": "x", "source_digest": SHA, "duration_seconds": 20, "request_count": 200, "concurrency": 4, "throughput_per_second": 10, "p95_latency_ms": 100, "p99_latency_ms": 200, "error_rate": 0, "cpu_peak_percent": 50, "memory_peak_percent": 50, "unknown_outcomes": 0}
        self.assertTrue(ops.capacity_evaluate(evidence, policy)["passed"])
        evidence["unknown_outcomes"] = 1
        self.assertFalse(ops.capacity_evaluate(evidence, policy)["passed"])

    def test_soak_thresholds(self):
        policy = {"duration_seconds": 10, "transaction_count": 10, "error_rate": .01, "memory_growth_mb_per_hour": 2, "fd_growth_per_hour": 1, "queue_lag_peak_seconds": 2, "unknown_outcomes": 0}
        evidence = {"evidence_id": "x", "source_digest": SHA, "duration_seconds": 20, "transaction_count": 100, "error_rate": 0, "memory_growth_mb_per_hour": 1, "fd_growth_per_hour": 0, "queue_lag_peak_seconds": 1, "unknown_outcomes": 0}
        self.assertTrue(ops.soak_evaluate(evidence, policy)["passed"])

    def test_observability_rejects_secret_log_field(self):
        policy = {"required_metrics": ["m"], "required_log_fields": ["timestamp"], "required_trace_spans": ["s"], "forbidden_log_fields": ["token"]}
        contract = {"metrics": ["m"], "log_fields": ["timestamp", "token"], "trace_spans": ["s"], "retention_days": 1, "trace_sampling_ratio": .1}
        self.assertFalse(ops.observability_validate(contract, policy)["passed"])

    def test_alert_routing(self):
        routes = {"last_tested_at": "2026-01-01T00:00:00Z", "routes": [{"severity": "critical", "primary": "a", "secondary": "b", "ack_minutes": 5, "escalation_after_minutes": 10}]}
        policy = {"required_severities": ["critical"], "maximum_ack_minutes": {"critical": 5}}
        self.assertTrue(ops.alert_routing_validate(routes, policy)["passed"])

    def test_data_governance_credentials_are_broker_only(self):
        policy = {"deletion_verification_required": True, "backup_max_retention_days": 1, "data_classes": [{"name": "credentials", "retention_days": 0, "deletion_method": "revoke", "contains_credentials": True, "storage": "ledger"}]}
        self.assertFalse(ops.data_governance_validate(policy)["passed"])

    def test_tabletop(self):
        exercise = {"participants": ["a", "b", "c"], "scenarios": [{"type": "x", "detection": "d", "containment": "c", "recovery": "r", "communications": "m", "lessons": "l", "score": 90}], "actions": [{"owner": "a", "due_date": "2026-01-01"}]}
        policy = {"required_scenarios": ["x"], "minimum_scenario_score": 80, "minimum_participants": 3}
        self.assertTrue(ops.incident_tabletop_evaluate(exercise, policy)["passed"])

    def test_approval_quorum_and_self_approval(self):
        record = {"requested_by": "requester", "release_digest": SHA, "approvals": [
            {"actor": "sec", "role": "security", "release_digest": SHA, "approval_digest": "b" * 64},
            {"actor": "ops", "role": "operations", "release_digest": SHA, "approval_digest": "c" * 64},
        ]}
        policy = {"minimum_approvals": 2, "required_roles": ["security", "operations"], "disallow_self_approval": True}
        self.assertTrue(ops.approvals_validate(record, policy)["passed"])
        record["approvals"][0]["actor"] = "requester"
        self.assertFalse(ops.approvals_validate(record, policy)["passed"])

    def test_evidence_catalog_rejects_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "real.json").write_text("{}\n")
            os.symlink("real.json", root / "link.json")
            spec = {"evidence": [{"id": "x", "path": "link.json", "required": True}]}
            self.assertFalse(ops.evidence_catalog(root, spec)["passed"])

    def test_bundle_is_deterministic_and_verifiable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.json").write_text('{"passed":true}\n')
            spec = {"evidence": [{"id": "a", "type": "test", "path": "a.json", "required": True}]}
            catalog = ops.evidence_catalog(root, spec)
            a = root / "a.zip"
            b = root / "b.zip"
            one = ops.certification_bundle(root, catalog, a, "bundle")
            two = ops.certification_bundle(root, catalog, b, "bundle")
            self.assertTrue(one["passed"])
            self.assertEqual(one["archive_sha256"], two["archive_sha256"])

    def test_bundle_detects_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "bad.zip"
            with zipfile.ZipFile(p, "w") as zf:
                zf.writestr("../bad", "x")
            self.assertFalse(ops.verify_certification_bundle(p)["passed"])

    def test_final_gate(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            paths = []
            for kind in ("a", "b"):
                p = root / f"{kind}.json"
                p.write_text(json.dumps({"kind": kind, "passed": True}) + "\n")
                paths.append(str(p))
            doc = ops.final_gate({"artifact_paths": paths}, ["a", "b"])
            self.assertTrue(doc["passed"])
            self.assertTrue(doc["external_certification_required"])

    def test_final_gate_fails_missing(self):
        self.assertFalse(ops.final_gate({"artifact_paths": []}, ["a"])["passed"])


if __name__ == "__main__":
    unittest.main()
