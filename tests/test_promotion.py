from __future__ import annotations

import datetime as dt
import importlib.util
import json
import tempfile
import unittest
import zipfile
from pathlib import Path

TOOL = Path(__file__).resolve().parents[1] / "tools" / "futurediff_promotion.py"
spec = importlib.util.spec_from_file_location("futurediff_promotion", TOOL)
promotion = importlib.util.module_from_spec(spec)
assert spec.loader
spec.loader.exec_module(promotion)

NOW = dt.datetime(2026, 7, 28, 12, 0, tzinfo=dt.timezone.utc)
SHA = "a" * 64


class PromotionTests(unittest.TestCase):
    def evidence_policy(self):
        return {
            "required_types": ["hosted-ci", "provider-effect"],
            "allowed_producers": ["github-actions", "certifier"],
            "allowed_sources": ["github-hosted-runner", "independent"],
            "environment": "production",
            "require_non_synthetic": True,
            "default_max_age_hours": 24,
            "max_future_skew_seconds": 60,
        }

    def build_evidence(self, root: Path, synthetic: bool = False):
        items = []
        for name, kind, producer, source in (
            ("ci.json", "hosted-ci", "github-actions", "github-hosted-runner"),
            ("effect.json", "provider-effect", "certifier", "independent"),
        ):
            path = root / name
            path.write_text(json.dumps({"passed": True, "kind": kind}) + "\n", encoding="utf-8")
            items.append({
                "id": kind,
                "type": kind,
                "path": name,
                "sha256": promotion.sha256_file(path),
                "producer": producer,
                "source": source,
                "environment": "production",
                "issued_at": "2026-07-28T11:00:00Z",
                "expires_at": "2026-07-29T11:00:00Z",
                "synthetic": synthetic,
            })
        return {"evidence": items}

    def test_external_evidence_intake_passes_real_fresh_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = promotion.evidence_intake(root, self.build_evidence(root), self.evidence_policy(), NOW)
            self.assertTrue(result["passed"])
            self.assertTrue(promotion.is_sha256(result["evidence_set_digest"]))

    def test_external_evidence_rejects_synthetic(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = promotion.evidence_intake(root, self.build_evidence(root, synthetic=True), self.evidence_policy(), NOW)
            self.assertFalse(result["passed"])

    def test_external_evidence_rejects_digest_change(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            specification = self.build_evidence(root)
            (root / "ci.json").write_text("changed\n", encoding="utf-8")
            result = promotion.evidence_intake(root, specification, self.evidence_policy(), NOW)
            self.assertFalse(result["passed"])

    def test_external_evidence_rejects_stale(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            specification = self.build_evidence(root)
            specification["evidence"][0]["issued_at"] = "2026-07-20T00:00:00Z"
            result = promotion.evidence_intake(root, specification, self.evidence_policy(), NOW)
            self.assertFalse(result["passed"])

    def oidc_policy(self):
        return {
            "issuer": "https://token.actions.githubusercontent.com",
            "allowed_audiences": ["futurediff-production"],
            "repository": "SHOnnay/futurediff",
            "allowed_workflow_refs": ["SHOnnay/futurediff/.github/workflows/release-promotion.yml@refs/heads/main"],
            "allowed_refs": ["refs/heads/main"],
            "allowed_events": ["workflow_dispatch"],
            "maximum_token_age_seconds": 900,
            "max_future_skew_seconds": 60,
            "denied_actors": [],
        }

    def oidc_claims(self):
        return {
            "iss": "https://token.actions.githubusercontent.com",
            "aud": "futurediff-production",
            "repository": "SHOnnay/futurediff",
            "workflow_ref": "SHOnnay/futurediff/.github/workflows/release-promotion.yml@refs/heads/main",
            "ref": "refs/heads/main",
            "event_name": "workflow_dispatch",
            "ref_protected": True,
            "sha": "a" * 40,
            "run_id": "123",
            "actor": "release-operator",
            "iat": "2026-07-28T11:55:00Z",
            "exp": "2026-07-28T12:10:00Z",
        }

    def test_oidc_claims_verify(self):
        result = promotion.oidc_claims_verify(self.oidc_claims(), self.oidc_policy(), NOW)
        self.assertTrue(result["passed"])

    def test_oidc_rejects_unprotected_ref(self):
        claims = self.oidc_claims()
        claims["ref_protected"] = False
        self.assertFalse(promotion.oidc_claims_verify(claims, self.oidc_policy(), NOW)["passed"])

    def exception_policy(self):
        return {
            "allowed_scopes": ["non-critical-documentation"],
            "allowed_risks": ["low"],
            "required_roles": ["security", "operations"],
            "maximum_duration_hours": 24,
            "minimum_rationale_length": 20,
            "minimum_compensating_controls": 2,
            "disallow_owner_approval": True,
        }

    def exception_record(self):
        return {
            "id": "EX-1",
            "owner": "owner",
            "scope": "non-critical-documentation",
            "risk": "low",
            "rationale": "A specific temporary exception with concrete boundaries.",
            "created_at": "2026-07-28T10:00:00Z",
            "expires_at": "2026-07-28T20:00:00Z",
            "compensating_controls": ["monitor", "auto-expire"],
            "approvals": [
                {"actor": "sec", "role": "security", "decision": "approved"},
                {"actor": "ops", "role": "operations", "decision": "approved"},
            ],
        }

    def test_exception_validate(self):
        self.assertTrue(promotion.exception_validate(self.exception_record(), self.exception_policy(), NOW)["passed"])

    def test_exception_rejects_owner_approval(self):
        record = self.exception_record()
        record["approvals"][0]["actor"] = "owner"
        self.assertFalse(promotion.exception_validate(record, self.exception_policy(), NOW)["passed"])

    def test_transparency_chain_and_tamper_detection(self):
        ledger = {"format_version": "1.0", "entries": []}
        first = {"recorded_at": "2026-07-28T12:00:00Z", "payload": {"kind": "promotion", "digest": SHA}}
        second = {"recorded_at": "2026-07-28T12:01:00Z", "payload": {"kind": "launch", "digest": "b" * 64}}
        ledger = promotion.transparency_append(ledger, first)
        ledger = promotion.transparency_append(ledger, second)
        self.assertTrue(promotion.transparency_verify(ledger)["passed"])
        ledger["entries"][0]["payload"]["digest"] = "c" * 64
        self.assertFalse(promotion.transparency_verify(ledger)["passed"])

    def test_transparency_rejects_duplicate_record(self):
        ledger = {"format_version": "1.0", "entries": []}
        record = {"recorded_at": "2026-07-28T12:00:00Z", "payload": {"kind": "promotion", "digest": SHA}}
        ledger = promotion.transparency_append(ledger, record)
        with self.assertRaises(promotion.PromotionError):
            promotion.transparency_append(ledger, record)

    def candidate(self):
        return {"approved": True, "version": "v1.55.0", "archive": "FutureDiff-v1.55.0-source.zip", "archive_sha256": SHA}

    def intake(self):
        return {"passed": True, "external_certification": True, "entries": [{"synthetic": False}], "evidence_set_digest": "b" * 64}

    def identity(self):
        return {"passed": True, "identity_digest": "c" * 64}

    def approvals(self):
        return {
            "passed": True,
            "release_digest": SHA,
            "approvals": [
                {"role": "security"},
                {"role": "operations"},
                {"role": "release-manager"},
            ],
        }

    def test_promotion_evaluate(self):
        policy = {"required_approval_roles": ["security", "operations", "release-manager"], "allow_exceptions": False, "allowed_exception_scopes": []}
        result = promotion.promotion_evaluate(self.candidate(), self.intake(), self.identity(), self.approvals(), policy)
        self.assertTrue(result["approved"])
        self.assertEqual(result["scope"], "external-production-promotion")

    def test_promotion_rejects_unbound_approval(self):
        approvals = self.approvals()
        approvals["release_digest"] = "d" * 64
        policy = {"required_approval_roles": ["security", "operations", "release-manager"], "allow_exceptions": False, "allowed_exception_scopes": []}
        self.assertFalse(promotion.promotion_evaluate(self.candidate(), self.intake(), self.identity(), approvals, policy)["passed"])

    def postdeploy_policy(self):
        return {
            "required_health_checks": ["api", "ledger"],
            "minimum_duration_seconds": 900,
            "minimum_availability": .999,
            "maximum_error_rate": .001,
            "maximum_p95_latency_ms": 500,
            "maximum_unknown_outcomes": 0,
            "maximum_unreconciled_effects": 0,
        }

    def postdeploy(self):
        return {
            "deployment_digest": SHA,
            "synthetic": False,
            "duration_seconds": 1200,
            "availability": 1,
            "error_rate": 0,
            "p95_latency_ms": 100,
            "unknown_outcomes": 0,
            "unreconciled_effects": 0,
            "health_checks": [{"name": "api", "passed": True}, {"name": "ledger", "passed": True}],
        }

    def test_postdeploy_health(self):
        self.assertTrue(promotion.postdeploy_evaluate(self.postdeploy(), self.postdeploy_policy())["passed"])

    def rollback(self):
        return {
            "rollback_plan_digest": SHA,
            "backup_digest": "b" * 64,
            "backup_verified": True,
            "synthetic": False,
            "last_drill_at": "2026-07-01T00:00:00Z",
            "last_drill_passed": True,
            "tested_rto_seconds": 100,
            "tested_rpo_seconds": 50,
            "current_metrics": {"error_rate": 0, "p95_latency_ms": 100, "unknown_outcomes": 0, "unreconciled_effects": 0},
        }

    def rollback_policy(self):
        return {
            "maximum_drill_age_days": 90,
            "maximum_rto_seconds": 600,
            "maximum_rpo_seconds": 300,
            "trigger_error_rate": .02,
            "trigger_p95_latency_ms": 2000,
            "trigger_unknown_outcomes": 0,
            "trigger_unreconciled_effects": 0,
        }

    def test_rollback_continue_and_trigger(self):
        result = promotion.rollback_evaluate(self.rollback(), self.rollback_policy(), NOW)
        self.assertTrue(result["passed"])
        self.assertEqual(result["decision"], "continue")
        evidence = self.rollback()
        evidence["current_metrics"]["error_rate"] = .1
        result = promotion.rollback_evaluate(evidence, self.rollback_policy(), NOW)
        self.assertEqual(result["decision"], "rollback")

    def test_launch_checklist(self):
        policy = {"runbook_acknowledged": True, "on_call_confirmed": True, "communications_ready": True}
        promo = {"approved": True, "passed": True, "promotion_digest": SHA}
        health = {"passed": True, "result_digest": "b" * 64}
        rollback = {"passed": True, "rollback_ready": True, "decision": "continue", "result_digest": "c" * 64}
        result = promotion.launch_checklist(promo, health, rollback, policy)
        self.assertTrue(result["production_complete"])

    def test_release_metadata(self):
        promo = {"approved": True, "archive_sha256": SHA, "promotion_digest": "b" * 64}
        ledger = promotion.transparency_append({"format_version": "1.0", "entries": []}, {"recorded_at": "2026-07-28T12:00:00Z", "payload": {"kind": "promotion"}})
        result = promotion.release_metadata(self.candidate(), promo, ledger)
        self.assertTrue(result["passed"])

    def test_promotion_bundle_deterministic_and_verifiable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.json").write_text('{"passed":true}\n', encoding="utf-8")
            (root / "b.json").write_text('{"passed":true}\n', encoding="utf-8")
            specification = {"artifacts": [{"id": "a", "path": "a.json"}, {"id": "b", "path": "b.json"}]}
            one = root / "one.zip"
            two = root / "two.zip"
            first = promotion.promotion_bundle(root, specification, one, "FutureDiff-promotion")
            second = promotion.promotion_bundle(root, specification, two, "FutureDiff-promotion")
            self.assertTrue(first["passed"])
            self.assertEqual(first["archive_sha256"], second["archive_sha256"])

    def test_promotion_bundle_rejects_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "bad.zip"
            with zipfile.ZipFile(path, "w") as archive:
                archive.writestr("../bad", "x")
            self.assertFalse(promotion.promotion_bundle_verify(path)["passed"])


if __name__ == "__main__":
    unittest.main()
