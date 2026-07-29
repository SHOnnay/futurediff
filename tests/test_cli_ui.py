from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import os
import tempfile
import unittest
import sys
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("cli_ui", ROOT / "tools" / "futurediff_cli_ui.py")
cli = importlib.util.module_from_spec(spec)
assert spec.loader
sys.modules[spec.name] = cli
spec.loader.exec_module(cli)


class CLIUITests(unittest.TestCase):
    def test_redacts_split_and_inline_secrets(self):
        args = ["run", "--token", "abc", "--api-key=xyz", "--name", "safe"]
        self.assertEqual(
            cli.redact_args(args),
            ["run", "--token", "<redacted>", "--api-key=<redacted>", "--name", "safe"],
        )

    def test_risky_detection(self):
        self.assertTrue(cli.is_risky(["access-cleanup", "--apply"]))
        self.assertTrue(cli.is_risky(["rollback-run"]))
        self.assertFalse(cli.is_risky(["transaction-list"]))

    def test_noninteractive_risky_requires_yes(self):
        caps = cli.Capabilities(color=False, unicode=False, interactive=False)
        with self.assertRaises(cli.UIError):
            cli.confirm_risky(["delete", "x"], yes=False, caps=caps)
        cli.confirm_risky(["delete", "x"], yes=True, caps=caps)

    def test_renderer_without_color_contains_no_ansi(self):
        stream = io.StringIO()
        renderer = cli.Renderer(cli.Capabilities(False, False, False), stream=stream)
        renderer.status("pass", "done")
        self.assertNotIn("\x1b", stream.getvalue())
        self.assertIn("done", stream.getvalue())

    def test_no_color_environment(self):
        with mock.patch.dict(os.environ, {"NO_COLOR": "1", "TERM": "xterm"}, clear=False):
            caps = cli.detect_capabilities(force_color=True)
        self.assertFalse(caps.color)

    def test_completion_generators(self):
        self.assertIn("complete -F", cli.completion_script("bash"))
        self.assertIn("#compdef", cli.completion_script("zsh"))
        self.assertIn("complete -c", cli.completion_script("fish"))
        with self.assertRaises(cli.UIError):
            cli.completion_script("unknown")

    def test_status_report_reads_closure_files(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "LOCAL_ENGINEERING_COMPLETION.json").write_text(
                json.dumps({"implementation_complete": True, "implementation_percentage": 100.0}) + "\n",
                encoding="utf-8",
            )
            (root / "EXTERNAL_PRODUCTION_CERTIFICATION_STATUS.json").write_text(
                json.dumps({"passed": False, "remaining_percentage": 9.0, "blocked_by": ["provider-certification"]}) + "\n",
                encoding="utf-8",
            )
            (root / "production-completion-decision.json").write_text(
                json.dumps({"production_complete": False, "failed": ["deployment-smoke"]}) + "\n",
                encoding="utf-8",
            )
            report = cli.status_report(root)
        self.assertTrue(report["local_engineering_complete"])
        self.assertFalse(report["production_complete"])
        self.assertGreater(report["remaining_percentage"], 0)
        self.assertIn("provider-certification", report["blocked_by"])
        self.assertIn("deployment-smoke", report["blocked_by"])

    def test_status_report_missing_files_is_safe(self):
        with tempfile.TemporaryDirectory() as d:
            report = cli.status_report(Path(d))
        self.assertFalse(report["production_complete"])
        self.assertEqual(report["remaining_percentage"], 0)

    def test_doctor_accepts_explicit_binary(self):
        binary = sys.executable
        with mock.patch.dict(os.environ, {"TERM": "xterm"}, clear=False):
            report = cli.doctor(binary)
        names = {x["name"]: x for x in report["checks"]}
        self.assertTrue(names["futurediff_binary"]["passed"])
        self.assertTrue(names["json_output"]["passed"])

    def test_command_payload_is_redacted(self):
        payload = cli.command_payload(["futurediff", "--token", "abc"], 0)
        self.assertEqual(payload["command"][-1], "<redacted>")
        self.assertTrue(payload["passed"])

    def test_run_command_preserves_exit_code(self):
        renderer = cli.Renderer(
            cli.Capabilities(False, False, False),
            quiet=True,
        )
        code = cli.run_command(
            sys.executable,
            ["-c", "import sys; sys.exit(7)"],
            renderer,
            json_mode=False,
            yes=True,
        )
        self.assertEqual(code, 7)

    def test_main_json_config_has_no_ansi(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = cli.main(["--json", "config"])
        self.assertEqual(code, 0)
        self.assertNotIn("\x1b", output.getvalue())
        self.assertEqual(json.loads(output.getvalue())["kind"], "futurediff-cli-ui-config")

    def test_main_direct_passthrough(self):
        with tempfile.TemporaryDirectory() as d:
            script = Path(d) / "exit_six.py"
            script.write_text(
                "raise SystemExit(6)\n",
                encoding="utf-8",
            )
            code = cli.main(
                [
                    "--binary",
                    sys.executable,
                    str(script),
                ]
            )
        self.assertEqual(code, 6)

    def test_main_noninteractive_risky_json_error(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = cli.main(
                [
                    "--json",
                    "--binary",
                    sys.executable,
                    "delete",
                    "x",
                ]
            )
        self.assertEqual(code, 2)
        result = json.loads(output.getvalue())
        self.assertFalse(result["passed"])
        self.assertIn("--yes", result["error"])

    def test_version_constant(self):
        self.assertEqual(cli.VERSION, "1.80.0")


if __name__ == "__main__":
    unittest.main()
