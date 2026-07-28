from __future__ import annotations

import datetime as dt
import importlib.util
import json
import tempfile
import unittest
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("closure", ROOT / "tools" / "futurediff_closure.py")
closure = importlib.util.module_from_spec(spec)
assert spec.loader
spec.loader.exec_module(closure)

SHA = "a" * 64
NOW = dt.datetime(2026, 7, 28, 12, 0, tzinfo=dt.timezone.utc)


class ClosureTests(unittest.TestCase):
    def test_integration_receipt_requires_merge_marker(self):
        with tempfile.TemporaryDirectory() as d:
            r = Path(d) / "repo"; r.mkdir(); (r / "go.mod").write_text("module x\n")
            b = Path(d) / "base.zip"; b.write_bytes(b"base")
            m = Path(d) / "manifest"; m.write_text("x\n")
            out = closure.integration_receipt(r, b, [m])
            self.assertFalse(out["passed"])
            marker = {"merged": True, "base_archive_sha256": closure.sha256_file(b), "overlay_manifests": [{"sha256": closure.sha256_file(m)}], "validated_at": "2026-07-28T00:00:00Z"}
            (r / ".futurediff-canonical-merge.json").write_text(json.dumps(marker))
            self.assertTrue(closure.integration_receipt(r, b, [m])["passed"])

    def test_archive_catalog_pass_and_missing(self):
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "a.zip"; p.write_bytes(b"x")
            expected = {"expected": {"a.zip": closure.sha256_file(p)}}
            self.assertTrue(closure.archive_catalog(Path(d), expected)["passed"])
            expected["expected"]["b.zip"] = SHA
            self.assertFalse(closure.archive_catalog(Path(d), expected)["passed"])

    def test_freshness_current_and_expired(self):
        spec = {"evidence": [{"id":"x","issued_at":"2026-07-28T00:00:00Z","expires_at":"2026-07-30T00:00:00Z"}]}
        self.assertTrue(closure.freshness_plan(spec, {"renew_before_hours": 12}, NOW)["passed"])
        spec["evidence"][0]["expires_at"] = "2026-07-28T01:00:00Z"
        self.assertFalse(closure.freshness_plan(spec, {"renew_before_hours": 12}, NOW)["passed"])

    def test_campaign_requires_targets_and_fields(self):
        good = {"required_targets":["a"],"targets":[{"id":"a","owner":"o","environment":"e","runner":"r","evidence_type":"t","command":"c"}]}
        self.assertTrue(closure.certification_campaign(good)["passed"])
        good["required_targets"].append("b")
        self.assertFalse(closure.certification_campaign(good)["passed"])

    def test_security_review_independence_and_findings(self):
        policy={"required_scope":["kernel"],"allowed_open_severities":["low"]}
        good={"subject_organization":"A","reviewer":{"organization":"B","independent":True},"scope":["kernel"],"findings":[],"report_sha256":SHA,"signed_by":"x","signed_at":"now","synthetic":False}
        self.assertTrue(closure.security_review(good,policy)["passed"])
        good["findings"]=[{"id":"F1","severity":"high","status":"open"}]
        self.assertFalse(closure.security_review(good,policy)["passed"])

    def test_load_soak_requires_real_measured_evidence(self):
        policy={"min_duration_hours":24,"min_request_count":100,"max_error_rate":.01,"max_p95_latency_ms":500,"max_memory_growth_pct":5,"max_unknown_outcomes":0}
        good={"synthetic":False,"duration_hours":24,"request_count":100,"metrics":{"error_rate":0,"p95_latency_ms":100,"memory_growth_pct":1,"unknown_outcomes":0},"evidence_sha256":SHA}
        self.assertTrue(closure.load_soak(good,policy)["passed"])
        good["synthetic"]=True
        self.assertFalse(closure.load_soak(good,policy)["passed"])

    def test_load_soak_threshold_failure(self):
        policy={"min_duration_hours":1,"min_request_count":1,"max_error_rate":0,"max_p95_latency_ms":1,"max_memory_growth_pct":0,"max_unknown_outcomes":0}
        bad={"synthetic":False,"duration_hours":1,"request_count":1,"metrics":{"error_rate":.1,"p95_latency_ms":2,"memory_growth_pct":1,"unknown_outcomes":1},"evidence_sha256":SHA}
        self.assertFalse(closure.load_soak(bad,policy)["passed"])

    def test_dr_evidence(self):
        good={"synthetic":False,"restore_success":True,"measured_rto_minutes":10,"measured_rpo_minutes":1,"integrity_verified":True,"executed_at":"x","evidence_sha256":SHA}
        self.assertTrue(closure.dr_evidence(good,{"max_rto_minutes":60,"max_rpo_minutes":5})["passed"])
        good["measured_rpo_minutes"]=10
        self.assertFalse(closure.dr_evidence(good,{"max_rto_minutes":60,"max_rpo_minutes":5})["passed"])

    def test_change_control(self):
        good={"release_id":"v","change_list_sha256":SHA,"freeze_starts_at":"2026-07-28T00:00:00Z","freeze_ends_at":"2026-07-29T00:00:00Z","emergency_override":False,"approvals":[{"role":"release","approved":True}]}
        self.assertTrue(closure.change_control(good,{"required_roles":["release"]},NOW)["passed"])
        good["emergency_override"]=True
        self.assertFalse(closure.change_control(good,{"required_roles":["release"]},NOW)["passed"])

    def test_credentials_metadata_only(self):
        good={"credentials":[{"id":"x","broker":"b","rotation_owner":"o","expires_at":"2027-01-01T00:00:00Z","scopes":["s"]}]}
        self.assertTrue(closure.credential_readiness(good,{"required_credentials":["x"]},NOW)["passed"])
        good["credentials"][0]["token"]="secret"
        self.assertFalse(closure.credential_readiness(good,{"required_credentials":["x"]},NOW)["passed"])

    def test_credentials_expiry(self):
        bad={"credentials":[{"id":"x","broker":"b","rotation_owner":"o","expires_at":"2026-01-01T00:00:00Z","scopes":["s"]}]}
        self.assertFalse(closure.credential_readiness(bad,{"required_credentials":["x"]},NOW)["passed"])

    def test_smoke_requires_real_checks(self):
        policy={"environment":"prod","required_checks":["health"]}
        good={"environment":"prod","archive_sha256":SHA,"evidence_sha256":SHA,"checks":[{"id":"health","passed":True,"synthetic":False}]}
        self.assertTrue(closure.smoke_test(good,policy)["passed"])
        good["checks"][0]["synthetic"]=True
        self.assertFalse(closure.smoke_test(good,policy)["passed"])

    def test_rollback_exercise(self):
        good={"synthetic":False,"triggered":True,"success":True,"duration_minutes":5,"state_integrity_verified":True,"forward_recovery_verified":True,"evidence_sha256":SHA}
        self.assertTrue(closure.rollback_exercise(good,{"max_duration_minutes":15})["passed"])
        good["success"]=False
        self.assertFalse(closure.rollback_exercise(good,{"max_duration_minutes":15})["passed"])

    def test_signoff_quorum(self):
        good={"release_owner":"owner","release_sha256":SHA,"on_call_confirmed":True,"communications_ready":True,"approvals":[{"actor":"a","role":"r1","approved":True},{"actor":"b","role":"r2","approved":True}]}
        self.assertTrue(closure.operational_signoff(good,{"required_roles":["r1","r2"],"minimum_distinct_approvers":2})["passed"])
        good["approvals"][0]["actor"]="owner"
        self.assertFalse(closure.operational_signoff(good,{"required_roles":["r1","r2"],"minimum_distinct_approvers":2})["passed"])

    def test_completion_requires_all_passed(self):
        rows=[{"kind":"a","passed":True,"result_digest":SHA},{"kind":"b","passed":True,"result_digest":SHA}]
        self.assertTrue(closure.completion_decision(rows,{"a","b"})["production_complete"])
        rows[1]["passed"]=False
        self.assertFalse(closure.completion_decision(rows,{"a","b"})["production_complete"])

    def test_completion_rejects_missing(self):
        out=closure.completion_decision([{"kind":"a","passed":True,"result_digest":SHA}],{"a","b"})
        self.assertFalse(out["passed"])
        self.assertIn("b",out["missing"])

    def test_bundle_deterministic(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)/"r";root.mkdir();(root/"a.txt").write_text("a")
            a=Path(d)/"a.zip";b=Path(d)/"b.zip"
            closure.deterministic_bundle(a,root,["a.txt"]);closure.deterministic_bundle(b,root,["a.txt"])
            self.assertEqual(a.read_bytes(),b.read_bytes())
            self.assertTrue(closure.verify_bundle(a)["passed"])

    def test_bundle_rejects_traversal(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d); (root/"a").write_text("a")
            with self.assertRaises(closure.ClosureError):
                closure.deterministic_bundle(root/"x.zip",root,["../a"])

    def test_bundle_verifier_detects_digest_mutation(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)/"r";root.mkdir();(root/"a").write_text("a")
            z=Path(d)/"x.zip";closure.deterministic_bundle(z,root,["a"])
            with zipfile.ZipFile(z,"a") as f:f.writestr("a",b"changed")
            self.assertFalse(closure.verify_bundle(z)["passed"])

    def test_canonical_json_stable(self):
        self.assertEqual(closure.canonical_bytes({"b":1,"a":2}),b'{"a":2,"b":1}\n')

    def test_safe_rel(self):
        self.assertTrue(closure.safe_rel("a/b"))
        self.assertFalse(closure.safe_rel("../a"))
        self.assertFalse(closure.safe_rel("/a"))

    def test_result_digest_changes(self):
        a=closure.result("x",[closure.check("a",True)])
        b=closure.result("x",[closure.check("a",False)])
        self.assertNotEqual(a["result_digest"],b["result_digest"])


if __name__ == "__main__":
    unittest.main()
