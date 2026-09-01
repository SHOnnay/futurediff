import ast
import os
import re
import subprocess
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def read_utf8(path: Path) -> str:
    """Read a repository text file with a platform-independent encoding."""
    return path.read_text(encoding="utf-8")


class PublicAlphaTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.version = read_utf8(ROOT / "VERSION").strip()

    def test_version_is_pre_one_alpha(self):
        self.assertRegex(
            self.version,
            r"^v0\.[0-9]+\.[0-9]+-alpha\.[0-9]+$",
        )
        self.assertEqual(self.version, "v0.1.0-alpha.3")

    def test_readme_does_not_lead_with_internal_progress(self):
        text = read_utf8(ROOT / "README.md").lower()
        for banned in (
            "task 180",
            "v1.80.0",
            "70 go binaries",
            "production implementation complete",
        ):
            self.assertNotIn(banned, text)
        self.assertIn("review ai-assisted code changes", text)
        self.assertIn("fdif finish --github", text)
        self.assertIn("show the starting screen", text)
        self.assertNotIn("| `fdif` | open the guided workflow |", text)

    def test_public_package_is_three_binaries(self):
        makefile = read_utf8(ROOT / "Makefile")
        self.assertIn("PUBLIC_COMMANDS := fdif futurediff futurediffd", makefile)
        public_line = makefile.split("PUBLIC_COMMANDS :=", 1)[1].splitlines()[0]
        self.assertNotIn("futurediff-mcp", public_line)

    def test_public_workflow_has_native_targets(self):
        text = read_utf8(ROOT / ".github/workflows/public-alpha-release.yml")
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
        self.assertIn(f"default: {self.version}", text)
        for path in (
            "docs/FDIF_GUIDED_CLI.md",
            "docs/FDIF_COMMAND_REFERENCE.md",
            "docs/adr/**",
        ):
            self.assertIn(path, text)

    def test_legacy_release_does_not_claim_v0_tags(self):
        text = read_utf8(ROOT / ".github/workflows/release.yml")
        self.assertIn("'v[1-9]*'", text)
        self.assertNotIn("- 'v*'", text)

    def test_installer_requires_checksum(self):
        text = read_utf8(ROOT / "scripts/install-release.sh")
        self.assertIn("sha256sum -c", text)
        self.assertIn("shasum -a 256 -c", text)
        self.assertNotIn("--no-verify", text)
        self.assertIn(self.version, text)

    @unittest.skipIf(
        os.name == "nt",
        "public alpha shell tooling targets Linux and macOS",
    )
    def test_script_help(self):
        for script in (
            "scripts/build-public-release.sh",
            "scripts/install-release.sh",
        ):
            subprocess.run(
                ["bash", script, "--help"],
                cwd=ROOT,
                check=True,
                stdout=subprocess.DEVNULL,
            )

    @unittest.skipIf(
        os.name == "nt",
        "public alpha installer targets Linux and macOS",
    )
    def test_installer_asset_resolution(self):
        result = subprocess.run(
            [
                "bash",
                "scripts/install-release.sh",
                "--version",
                self.version,
                "--print-asset",
            ],
            cwd=ROOT,
            check=True,
            text=True,
            capture_output=True,
        )
        escaped = re.escape(self.version)
        self.assertRegex(
            result.stdout.strip(),
            rf"^futurediff-{escaped}-(linux|darwin)-(amd64|arm64)\.tar\.gz$",
        )

    def test_public_alpha_contract_is_enforced(self):
        makefile = read_utf8(ROOT / "Makefile")
        workflow = read_utf8(
            ROOT / ".github/workflows/public-alpha-release.yml"
        )
        command = (
            "python3 -m unittest discover -s tests "
            "-p 'test_public_alpha.py' -v"
        )
        self.assertIn("public-alpha-test:", makefile)
        self.assertIn("$(MAKE) public-alpha-test", makefile)
        self.assertIn(command, makefile)
        self.assertIn(command, workflow)

    def test_public_package_verification_is_directory_safe(self):
        makefile = read_utf8(ROOT / "Makefile")
        readme = read_utf8(ROOT / "README.md")
        install = read_utf8(ROOT / "docs/FDIF_INSTALLATION.md")
        self.assertIn("verify-public-package:", makefile)
        self.assertIn('cd "$$out"', makefile)
        self.assertIn("sha256sum -c ./*.sha256", makefile)
        self.assertIn("shasum -a 256 -c ./*.sha256", makefile)
        self.assertIn("make verify-public-package", readme)
        self.assertIn("make verify-public-package", install)

    def test_public_archive_rejects_platform_metadata(self):
        script = read_utf8(ROOT / "scripts/build-public-release.sh")
        archiver = read_utf8(ROOT / "scripts/reproducible-archive.py")
        self.assertIn("scripts/reproducible-archive.py", script)
        self.assertIn("SOURCE_DATE_EPOCH", script)
        self.assertIn('git -C "$root" log -1 --format=%ct HEAD', script)
        self.assertIn('--mtime "$sde"', script)
        # The archiver must normalize every nondeterministic archive field.
        self.assertIn("sorted path order", archiver)
        self.assertIn("uid/gid are forced to 0", archiver)
        self.assertIn("uname/gname to root/root", archiver)
        self.assertIn("mtime is forced to SOURCE_DATE_EPOCH", archiver)
        self.assertIn("no atime/ctime", archiver)
        self.assertIn("mtime = SOURCE_DATE_EPOCH and no original", archiver)
        self.assertIn("tarfile.GNU_FORMAT", archiver)
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

    def test_public_release_script_avoids_pipefail_version_checks(self):
        script = read_utf8(ROOT / "scripts/build-public-release.sh")
        self.assertIn("verify_binary_version()", script)
        self.assertIn(
            "public binary version output does not include $version",
            script,
        )
        self.assertIn('verify_binary_version "$stage/bin/fdif" version', script)
        self.assertIn(
            'verify_binary_version "$stage/bin/futurediff" version',
            script,
        )
        self.assertIn(
            'verify_binary_version "$stage/bin/futurediffd" --version',
            script,
        )
        self.assertNotIn('"$stage/bin/fdif" version | grep -Fq "$version"', script)
        self.assertNotIn(
            '"$stage/bin/futurediff" version | grep -Fq "$version"',
            script,
        )
        self.assertNotIn(
            '"$stage/bin/futurediffd" --version | grep -Fq "$version"',
            script,
        )

    def test_repository_text_reads_use_explicit_encoding(self):
        tree = ast.parse(read_utf8(Path(__file__)))
        unqualified = []
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            if not isinstance(node.func, ast.Attribute):
                continue
            if node.func.attr != "read_text":
                continue
            has_encoding = any(
                keyword.arg == "encoding" for keyword in node.keywords
            )
            if not has_encoding:
                unqualified.append(node.lineno)
        self.assertEqual(
            unqualified,
            [],
            f"read_text calls without explicit encoding: {unqualified}",
        )

    def test_document_links_exist(self):
        for path in (
            "README.md",
            "SECURITY.md",
            "ROADMAP.md",
            "docs/QUICKSTART.md",
            "docs/LIMITATIONS.md",
            "docs/THREAT_MODEL.md",
            "docs/FDIF_INSTALLATION.md",
            "docs/FDIF_GUIDED_CLI.md",
            "docs/FDIF_COMMAND_REFERENCE.md",
            "docs/FDIF_GITHUB_PUBLICATION.md",
            "docs/adr/0003-fdif-home-and-path-canonicalization.md",
        ):
            self.assertTrue((ROOT / path).exists(), path)

    def test_first_run_and_path_ux_contract(self):
        app = read_utf8(ROOT / "internal/guidedcli/app.go")
        paths = read_utf8(ROOT / "internal/guidedcli/paths.go")
        main = read_utf8(ROOT / "cmd/fdif/main.go")
        guided = read_utf8(ROOT / "docs/FDIF_GUIDED_CLI.md")

        self.assertIn("a.landing()", app)
        self.assertIn('case "menu":', app)
        self.assertNotIn("no command supplied", app)
        self.assertIn("fdif config --explain", app)
        self.assertIn("Your current branch was not modified", app)
        self.assertIn("if a.Verbose", app)

        self.assertIn('os.Getenv("FDIF_HOME")', paths)
        self.assertIn('os.Getenv("FUTUREDIFF_ROOT")', paths)
        self.assertIn('"--state (advanced)"', paths)
        self.assertIn('path == "/tmp"', paths)
        self.assertIn("refusing home path that is itself a symlink", paths)

        for flag in ("--home", "--root", "--verbose"):
            self.assertIn(flag, main)
        self.assertIn("This behavior is the same when stdout is not a terminal", guided)
        self.assertIn("FDIF_HOME", guided)
        self.assertIn("/private/tmp", guided)


    def test_guided_git_subprocess_boundary_contract(self):
        app = read_utf8(ROOT / "internal/guidedcli/app.go")
        security = read_utf8(ROOT / "SECURITY.md")
        threat = read_utf8(ROOT / "docs/THREAT_MODEL.md")
        guided = read_utf8(ROOT / "docs/FDIF_GUIDED_CLI.md")

        self.assertIn('"GIT_CONFIG_NOSYSTEM=1"', app)
        self.assertIn('"GIT_CONFIG_GLOBAL=" + os.DevNull', app)
        self.assertIn('"GIT_TERMINAL_PROMPT=0"', app)
        self.assertIn('"rev-parse", "--is-bare-repository"', app)
        self.assertIn('"symbolic-ref", "-q", "HEAD"', app)
        self.assertIn('"%s is a bare Git repository; use a checked-out worktree on a branch"', app)
        self.assertIn('"%s has a detached HEAD; checkout a branch before running fdif start"', app)
        self.assertIn('"-c", "core.fsmonitor=false"', app)
        self.assertIn('"-c", "credential.helper="', app)
        self.assertIn('"-c", "core.hooksPath=/dev/null"', app)

        self.assertIn("ambient `GIT_DIR`, `GIT_WORK_TREE`", security)
        self.assertIn("rejects bare repositories and detached HEAD states", security)
        self.assertIn("Git config, environment, or replacement-object injection", threat)
        self.assertIn("Detached, bare, shallow, history-substituted, or unsupported repository shape", threat)
        self.assertIn("disable hooks and fsmonitor", guided)
        self.assertIn("rejects bare repositories and detached HEAD states", guided)

if __name__ == "__main__":
    unittest.main()
