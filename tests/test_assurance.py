from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path

MODULE_PATH = Path(__file__).resolve().parents[1] / "tools" / "futurediff_assurance.py"
spec = importlib.util.spec_from_file_location("futurediff_assurance", MODULE_PATH)
assert spec and spec.loader
fd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fd)

MIT = """MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\nof this software and associated documentation files (the \"Software\"), to deal\nin the Software without restriction.\n"""

class AssuranceTests(unittest.TestCase):
    def make_repo(self, base: Path) -> Path:
        root = base / "repo"
        root.mkdir()
        (root / "README.md").write_text("# demo\n", encoding="utf-8")
        (root / "LICENSE").write_text(MIT, encoding="utf-8")
        (root / "config.json").write_text('{"safe":true}\n', encoding="utf-8")
        return root

    def test_canonical_json_is_stable(self):
        self.assertEqual(fd.canonical_json_bytes({"b": 2, "a": 1}), b'{"a":1,"b":2}\n')

    def test_manifest_round_trip(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            manifest = fd.build_manifest(root)
            result = fd.verify_manifest(root, manifest)
            self.assertTrue(result["verified"])
            self.assertEqual(manifest["file_count"], 3)

    def test_manifest_detects_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            manifest = fd.build_manifest(root)
            (root / "README.md").write_text("changed\n", encoding="utf-8")
            result = fd.verify_manifest(root, manifest)
            self.assertFalse(result["verified"])
            self.assertIn("README.md", result["changed"])

    def test_manifest_rejects_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            os.symlink("README.md", root / "link")
            with self.assertRaises(fd.AssuranceError):
                fd.build_manifest(root)

    def test_sbom_round_trip(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            sbom = fd.create_sbom(root, "demo", "1.0.0")
            self.assertEqual(sbom["bomFormat"], "CycloneDX")
            self.assertTrue(fd.verify_sbom(root, sbom)["verified"])

    def test_sbom_detects_unexpected_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            sbom = fd.create_sbom(root, "demo", "1.0.0")
            (root / "new.txt").write_text("new", encoding="utf-8")
            result = fd.verify_sbom(root, sbom)
            self.assertFalse(result["verified"])
            self.assertIn("new.txt", result["unexpected"])

    def test_provenance_round_trip(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            manifest = fd.build_manifest(root)
            provenance = fd.create_provenance(root, manifest, "demo", "1.0.0", "builder", "git+https://example.invalid/repo", "0" * 40)
            result = fd.verify_provenance(manifest, provenance)
            self.assertTrue(result["verified"])

    def test_secret_scan_fingerprint_only(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            token = "gh" + "p_" + ("A" * 24)
            (root / "oops.txt").write_text(token, encoding="utf-8")
            result = fd.secret_scan(root)
            self.assertFalse(result["clean"])
            encoded = json.dumps(result)
            self.assertNotIn(token, encoded)
            self.assertEqual(result["findings"][0]["kind"], "github_token")

    def test_license_scan_approves_mit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            policy = {"allowed_spdx": ["MIT"], "denied_module_prefixes": [], "require_repository_license": True}
            result = fd.license_scan(root, policy)
            self.assertTrue(result["approved"])
            self.assertEqual(result["repository_license"], "MIT")

    def test_license_scan_denies_module_prefix(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            (root / "go.mod").write_text("module demo\n\nrequire bad.example/module v1.2.3\n", encoding="utf-8")
            policy = {"allowed_spdx": ["MIT"], "denied_module_prefixes": ["bad.example/"], "require_repository_license": True}
            result = fd.license_scan(root, policy)
            self.assertFalse(result["approved"])
            self.assertEqual(len(result["denied_modules"]), 1)

    def test_readiness_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            (root / "docs").mkdir()
            policy = {
                "required_files": ["README.md", "LICENSE"],
                "required_directories": ["docs"],
                "commands": [["python3", "-c", "print('ok')"]],
                "command_timeout_seconds": 5,
                "required_external_evidence": [],
            }
            result = fd.readiness(root, policy)
            self.assertTrue(result["approved"])

    def test_readiness_fails_missing_external_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            policy = {"required_files": [], "required_directories": [], "commands": [], "required_external_evidence": ["docker"], "external_evidence_report": "evidence.json"}
            result = fd.readiness(root, policy)
            self.assertFalse(result["approved"])
            self.assertEqual(result["external_evidence"]["missing"], ["docker"])

    def test_slo_pass_and_fail(self):
        policy = {
            "minimum_availability": 0.999,
            "maximum_p95_latency_ms": 250,
            "maximum_error_rate": 0.001,
            "maximum_unknown_outcomes": 0,
            "maximum_restore_rpo_seconds": 300,
            "maximum_restore_rto_seconds": 900,
        }
        good = {"availability": 0.9999, "p95_latency_ms": 100, "error_rate": 0.0, "unknown_outcomes": 0, "restore_rpo_seconds": 60, "restore_rto_seconds": 100}
        bad = dict(good, unknown_outcomes=1)
        self.assertTrue(fd.slo_evaluate(good, policy)["approved"])
        self.assertFalse(fd.slo_evaluate(bad, policy)["approved"])

    def test_backup_restore_round_trip(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            source = self.make_repo(base)
            archive = base / "backup.tar.gz"
            fd.backup_create(source, archive)
            result = fd.backup_restore(archive, base / "restored")
            self.assertTrue(result["restored"])
            self.assertEqual((base / "restored" / "README.md").read_text(), "# demo\n")

    def test_backup_restore_rejects_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            archive = base / "bad.tar.gz"
            with tarfile.open(archive, "w:gz") as tar:
                data = b"x"
                info = tarfile.TarInfo("payload/../../outside")
                info.size = len(data)
                import io
                tar.addfile(info, io.BytesIO(data))
            with self.assertRaises(fd.AssuranceError):
                fd.backup_restore(archive, base / "restored")

    def test_recovery_drill(self):
        self.assertTrue(fd.recovery_drill()["passed"])

    def test_chaos_suite(self):
        result = fd.chaos_run()
        self.assertTrue(result["passed"])
        self.assertEqual(len(result["checks"]), 5)

    def test_deterministic_release_zip(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            root = self.make_repo(base)
            a = base / "a.zip"
            b = base / "b.zip"
            first = fd.deterministic_zip(root, a, "demo-1")
            second = fd.deterministic_zip(root, b, "demo-1")
            self.assertEqual(first["sha256"], second["sha256"])
            self.assertTrue(fd.verify_release_zip(a)["verified"])

    def test_release_zip_detects_unexpected_member(self):
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            root = self.make_repo(base)
            archive = base / "release.zip"
            fd.deterministic_zip(root, archive, "demo-1")
            with zipfile.ZipFile(archive, "a") as zf:
                zf.writestr("demo-1/unexpected.txt", "bad")
            result = fd.verify_release_zip(archive)
            self.assertFalse(result["verified"])
            self.assertIn("unexpected.txt", result["unexpected"])

    def test_openssl_signature_round_trip(self):
        if not shutil_which("openssl"):
            self.skipTest("openssl unavailable")
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            private_key = base / "private.pem"
            public_key = base / "public.pem"
            payload = base / "payload.txt"
            signature = base / "payload.sig"
            payload.write_text("signed payload\n", encoding="utf-8")
            subprocess.run(["openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", str(private_key)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            subprocess.run(["openssl", "pkey", "-in", str(private_key), "-pubout", "-out", str(public_key)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            fd.openssl_sign(payload, private_key, signature)
            self.assertTrue(fd.openssl_verify(payload, public_key, signature)["verified"])
            payload.write_text("tampered\n", encoding="utf-8")
            self.assertFalse(fd.openssl_verify(payload, public_key, signature)["verified"])

    def test_parse_go_mod(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_repo(Path(tmp))
            (root / "go.mod").write_text("module demo\n\nrequire (\n example.com/a v1.0.0\n example.com/b v2.0.0 // indirect\n)\n", encoding="utf-8")
            deps = fd.parse_go_mod(root)
            self.assertEqual([d["name"] for d in deps], ["example.com/a", "example.com/b"])


def shutil_which(name: str):
    import shutil
    return shutil.which(name)

if __name__ == "__main__":
    unittest.main()
