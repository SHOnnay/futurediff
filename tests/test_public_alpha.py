import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class PublicAlphaTests(unittest.TestCase):
    def test_version_is_pre_one_alpha(self):
        self.assertEqual((ROOT / "VERSION").read_text().strip(), "v0.1.0-alpha.1")

    def test_readme_does_not_lead_with_internal_progress(self):
        text = (ROOT / "README.md").read_text().lower()
        for banned in (
            "task 180",
            "v1.80.0",
            "70 go binaries",
            "production implementation complete",
        ):
            self.assertNotIn(banned, text)
        self.assertIn("review ai-assisted code changes", text)
        self.assertIn("fdif finish --github", text)

    def test_public_package_is_three_binaries(self):
        makefile = (ROOT / "Makefile").read_text()
        self.assertIn("PUBLIC_COMMANDS := fdif futurediff futurediffd", makefile)
        public_line = makefile.split("PUBLIC_COMMANDS :=", 1)[1].splitlines()[0]
        self.assertNotIn("futurediff-mcp", public_line)

    def test_public_workflow_has_native_targets(self):
        text = (ROOT / ".github/workflows/public-alpha-release.yml").read_text()
        for target in (
            "linux-amd64",
            "linux-arm64",
            "darwin-arm64",
            "darwin-amd64",
        ):
            self.assertIn(f"target: {target}", text)
        self.assertIn("ubuntu-24.04-arm", text)
        self.assertIn("macos-15-intel", text)
        self.assertIn("'v0.*'", text)
        self.assertIn("artifact-metadata: write", text)
        self.assertIn("if: github.ref_type == 'tag'", text)
        self.assertIn("INPUT_VERSION: ${{ inputs.version }}", text)

    def test_legacy_release_does_not_claim_v0_tags(self):
        text = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertIn("'v[1-9]*'", text)
        self.assertNotIn("- 'v*'", text)

    def test_installer_requires_checksum(self):
        text = (ROOT / "scripts/install-release.sh").read_text()
        self.assertIn("sha256sum -c", text)
        self.assertIn("shasum -a 256 -c", text)
        self.assertNotIn("--no-verify", text)

    def test_script_help_and_asset_resolution(self):
        subprocess.run(
            ["bash", str(ROOT / "scripts/build-public-release.sh"), "--help"],
            check=True,
            stdout=subprocess.DEVNULL,
        )
        subprocess.run(
            ["bash", str(ROOT / "scripts/install-release.sh"), "--help"],
            check=True,
            stdout=subprocess.DEVNULL,
        )
        result = subprocess.run(
            [
                "bash",
                str(ROOT / "scripts/install-release.sh"),
                "--version",
                "v0.1.0-alpha.1",
                "--print-asset",
            ],
            check=True,
            text=True,
            capture_output=True,
        )
        self.assertRegex(
            result.stdout.strip(),
            r"^futurediff-v0\.1\.0-alpha\.1-(linux|darwin)-(amd64|arm64)\.tar\.gz$",
        )

    def test_public_alpha_contract_is_enforced(self):
        makefile = (ROOT / "Makefile").read_text()
        workflow = (
            ROOT / ".github/workflows/public-alpha-release.yml"
        ).read_text()
        command = (
            "python3 -m unittest discover -s tests "
            "-p 'test_public_alpha.py' -v"
        )
        self.assertIn("public-alpha-test:", makefile)
        self.assertIn("$(MAKE) public-alpha-test", makefile)
        self.assertIn(command, makefile)
        self.assertIn(command, workflow)

    def test_public_package_verification_is_directory_safe(self):
        makefile = (ROOT / "Makefile").read_text()
        readme = (ROOT / "README.md").read_text()
        install = (ROOT / "docs/FDIF_INSTALLATION.md").read_text()
        self.assertIn("verify-public-package:", makefile)
        self.assertIn('cd "$$out"', makefile)
        self.assertIn("sha256sum -c ./*.sha256", makefile)
        self.assertIn("shasum -a 256 -c ./*.sha256", makefile)
        self.assertIn("make verify-public-package", readme)
        self.assertIn("make verify-public-package", install)

    def test_public_archive_rejects_platform_metadata(self):
        script = (ROOT / "scripts/build-public-release.sh").read_text()
        self.assertIn("COPYFILE_DISABLE=1 tar -czf", script)
        self.assertIn("-name '._*'", script)
        self.assertIn("-name '.DS_Store'", script)
        self.assertIn("unexpected symlink in public staging tree", script)
        self.assertIn(
            "public archive contains missing or unexpected entries",
            script,
        )
        for required in (
            "$name/LICENSE",
            "$name/README.md",
            "$name/VERSION",
            "$name/bin/fdif",
            "$name/bin/futurediff",
            "$name/bin/futurediffd",
            "$name/completions/_fdif",
            "$name/completions/fdif.bash",
            "$name/completions/fdif.fish",
            "$name/completions/fdif.ps1",
        ):
            self.assertIn(required, script)

    def test_document_links_exist(self):
        for path in (
            "README.md",
            "SECURITY.md",
            "ROADMAP.md",
            "docs/QUICKSTART.md",
            "docs/LIMITATIONS.md",
            "docs/FDIF_INSTALLATION.md",
            "docs/FDIF_GUIDED_CLI.md",
            "docs/FDIF_COMMAND_REFERENCE.md",
            "docs/FDIF_GITHUB_PUBLICATION.md",
        ):
            self.assertTrue((ROOT / path).exists(), path)


if __name__ == "__main__":
    unittest.main()
