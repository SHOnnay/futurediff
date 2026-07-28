#!/usr/bin/env python3
"""Clean terminal interface for the FutureDiff CLI.

Dependency-free wrapper that preserves the canonical CLI's stdout, stderr,
signals, and exit codes while adding readable interactive output, structured
wrapper JSON, diagnostics, completion generation, and safety confirmations.
"""
from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence

VERSION = "1.80.0"
DEFAULT_BINARY = "futurediff"
SECRET_FLAG = re.compile(r"(?i)(token|secret|password|passwd|api[-_]?key|authorization|credential)")
RISKY_WORDS = {"delete", "destroy", "revoke", "cleanup", "rollback", "purge", "reset", "drop"}


class UIError(RuntimeError):
    """Expected terminal UI failure."""


@dataclass(frozen=True)
class Capabilities:
    color: bool
    unicode: bool
    interactive: bool


def _truthy(value: str | None) -> bool:
    return bool(value and value.strip().lower() not in {"0", "false", "no", "off"})


def detect_capabilities(*, no_color: bool = False, force_color: bool = False) -> Capabilities:
    interactive = bool(sys.stdout.isatty() and sys.stdin.isatty() and not _truthy(os.getenv("CI")))
    term = os.getenv("TERM", "")
    color = bool(
        not no_color
        and not os.getenv("NO_COLOR")
        and term.lower() != "dumb"
        and (force_color or sys.stdout.isatty())
    )
    encoding = (getattr(sys.stdout, "encoding", None) or "").lower()
    unicode_ok = "utf" in encoding and term.lower() != "dumb"
    return Capabilities(color=color, unicode=unicode_ok, interactive=interactive)


class Renderer:
    RESET = "\033[0m"
    BOLD = "\033[1m"
    DIM = "\033[2m"
    GREEN = "\033[32m"
    YELLOW = "\033[33m"
    RED = "\033[31m"
    CYAN = "\033[36m"

    def __init__(self, caps: Capabilities, *, quiet: bool = False, stream: Any = None) -> None:
        self.caps = caps
        self.quiet = quiet
        self.stream = stream or sys.stderr

    def style(self, text: str, *codes: str) -> str:
        if not self.caps.color:
            return text
        return "".join(codes) + text + self.RESET

    def line(self, text: str = "", *, force: bool = False) -> None:
        if not self.quiet or force:
            print(text, file=self.stream)

    def heading(self, text: str) -> None:
        self.line(self.style(text, self.BOLD, self.CYAN))

    def status(self, state: str, text: str) -> None:
        normalized = state.lower()
        if normalized in {"pass", "passed", "ok", "ready", "complete"}:
            symbol, code = ("✓" if self.caps.unicode else "OK"), self.GREEN
        elif normalized in {"warn", "warning", "blocked", "pending"}:
            symbol, code = ("!" if self.caps.unicode else "WARN"), self.YELLOW
        else:
            symbol, code = ("✗" if self.caps.unicode else "ERR"), self.RED
        self.line(f"{self.style(symbol, self.BOLD, code)} {text}")

    def table(self, rows: Sequence[tuple[str, str]]) -> None:
        if not rows:
            return
        width = max(len(str(k)) for k, _ in rows)
        for key, value in rows:
            self.line(f"{self.style(str(key).ljust(width), self.DIM)}  {value}")


def redact_args(args: Sequence[str]) -> list[str]:
    """Return command arguments with likely credential values removed."""
    output: list[str] = []
    hide_next = False
    for arg in args:
        if hide_next:
            output.append("<redacted>")
            hide_next = False
            continue
        if arg.startswith("--") and "=" in arg:
            key, _value = arg.split("=", 1)
            output.append(f"{key}=<redacted>" if SECRET_FLAG.search(key) else arg)
            continue
        output.append(arg)
        if arg.startswith("-") and SECRET_FLAG.search(arg):
            hide_next = True
    return output


def resolve_binary(value: str) -> str:
    if os.sep in value or (os.altsep and os.altsep in value):
        path = Path(value).expanduser()
        if not path.is_file():
            raise UIError(f"FutureDiff binary not found: {path}")
        return str(path)
    found = shutil.which(value)
    if not found:
        raise UIError(f"FutureDiff binary '{value}' is not available on PATH")
    return found


def is_risky(args: Sequence[str]) -> bool:
    for raw in args:
        pieces = set(re.split(r"[-_:/.]+", raw.strip().lower().lstrip("-")))
        if pieces & RISKY_WORDS:
            return True
    return False


def confirm_risky(args: Sequence[str], *, yes: bool, caps: Capabilities) -> None:
    if not is_risky(args) or yes:
        return
    if not caps.interactive:
        raise UIError("risky command requires --yes in non-interactive mode")
    shown = " ".join(redact_args(args))
    answer = input(f"Run potentially destructive command '{shown}'? Type YES: ").strip()
    if answer != "YES":
        raise UIError("command cancelled")


def command_payload(command: Sequence[str], returncode: int) -> dict[str, Any]:
    return {
        "format_version": "1.0",
        "kind": "futurediff-cli-ui-execution",
        "command": redact_args(command),
        "returncode": int(returncode),
        "passed": returncode == 0,
    }


def run_command(binary: str, args: Sequence[str], renderer: Renderer, *, json_mode: bool, yes: bool) -> int:
    confirm_risky(args, yes=yes, caps=renderer.caps)
    command = [resolve_binary(binary), *args]
    if not json_mode:
        renderer.heading("FutureDiff")
        renderer.line("Command: " + " ".join(redact_args(command)))
    try:
        returncode = subprocess.Popen(command).wait()
    except KeyboardInterrupt:
        returncode = 130
    if json_mode:
        return returncode
    elif returncode == 0:
        renderer.status("pass", "Command completed")
    else:
        renderer.status("fail", f"Command failed with exit code {returncode}")
    return returncode


def _check(name: str, ok: bool, detail: str) -> dict[str, Any]:
    return {"name": name, "passed": bool(ok), "detail": detail}


def doctor(binary: str, root: Path | None = None) -> dict[str, Any]:
    checks: list[dict[str, Any]] = []
    found = shutil.which(binary) if os.sep not in binary else (binary if Path(binary).is_file() else None)
    checks.append(_check("futurediff_binary", bool(found), str(found or "not found")))
    checks.append(_check("git", bool(shutil.which("git")), shutil.which("git") or "not found"))
    checks.append(_check("python", sys.version_info >= (3, 10), platform.python_version()))
    checks.append(_check("terminal", bool(os.getenv("TERM") or os.name == "nt"), os.getenv("TERM", os.name)))
    checks.append(_check("json_output", True, "supported"))
    checks.append(_check("color_opt_out", True, "NO_COLOR and --no-color supported"))
    if root is not None:
        expanded = root.expanduser()
        checks.append(_check("state_root_exists", expanded.is_dir(), str(expanded)))
        if expanded.exists():
            checks.append(_check("state_root_writable", os.access(expanded, os.W_OK), str(expanded)))
    for runtime in ("docker", "podman"):
        path = shutil.which(runtime)
        checks.append(_check(runtime, bool(path), path or "optional runtime not found"))
    required = [c for c in checks if c["name"] not in {"docker", "podman"}]
    return {
        "format_version": "1.0",
        "kind": "futurediff-cli-doctor",
        "passed": all(c["passed"] for c in required),
        "platform": platform.platform(),
        "checks": checks,
    }


def render_doctor(report: dict[str, Any], renderer: Renderer) -> None:
    renderer.heading("FutureDiff CLI doctor")
    for item in report["checks"]:
        state = "pass" if item["passed"] else ("warn" if item["name"] in {"docker", "podman"} else "fail")
        renderer.status(state, f"{item['name']}: {item['detail']}")


def _read_json(path: Path) -> dict[str, Any] | None:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return value if isinstance(value, dict) else None


def status_report(status_dir: Path) -> dict[str, Any]:
    local_path = status_dir / "LOCAL_ENGINEERING_COMPLETION.json"
    external_path = status_dir / "EXTERNAL_PRODUCTION_CERTIFICATION_STATUS.json"
    decision_path = status_dir / "production-completion-decision.json"
    local = _read_json(local_path)
    external = _read_json(external_path)
    decision = _read_json(decision_path)
    blocked: list[str] = []
    if external:
        blocked.extend(str(x) for x in external.get("blocked_by", []))
    if decision:
        for value in decision.get("failed", []):
            if str(value) not in blocked:
                blocked.append(str(value))
    return {
        "format_version": "1.0",
        "kind": "futurediff-cli-status",
        "status_directory": str(status_dir),
        "local_engineering_complete": bool(local and local.get("implementation_complete")),
        "local_engineering_percentage": float((local or {}).get("implementation_percentage", 0.0)),
        "external_certification_passed": bool(external and external.get("passed")),
        "production_complete": bool(decision and decision.get("production_complete")),
        "remaining_percentage": float((external or {}).get("remaining_percentage", 0.0)),
        "blocked_by": blocked,
        "files": {
            "local": local_path.is_file(),
            "external": external_path.is_file(),
            "decision": decision_path.is_file(),
        },
    }


def render_status(report: dict[str, Any], renderer: Renderer) -> None:
    renderer.heading("FutureDiff status")
    renderer.table([
        ("Local engineering", f"{report['local_engineering_percentage']:.1f}%"),
        ("External remaining", f"{report['remaining_percentage']:.1f}%"),
        ("Production complete", "yes" if report["production_complete"] else "no"),
    ])
    renderer.line()
    renderer.status("pass" if report["local_engineering_complete"] else "fail", "Local product and control implementation")
    renderer.status("pass" if report["external_certification_passed"] else "blocked", "External certification")
    if report["blocked_by"]:
        renderer.line()
        renderer.heading("Next required evidence")
        for item in report["blocked_by"]:
            renderer.line(f"- {item}")


def completion_script(shell: str, program: str = "futurediff-ui") -> str:
    commands = "doctor status config completion help"
    function_name = program.replace("-", "_")
    if shell == "bash":
        return (
            f"_{function_name}_complete() {{\n"
            '  local cur="${COMP_WORDS[COMP_CWORD]}"\n'
            f'  COMPREPLY=( $(compgen -W "{commands}" -- "$cur") )\n'
            "}\n"
            f"complete -F _{function_name}_complete {program}\n"
        )
    if shell == "zsh":
        return f"#compdef {program}\n_arguments '1:command:({commands})' '*::arguments:->args'\n"
    if shell == "fish":
        return "\n".join(f"complete -c {program} -f -a {cmd}" for cmd in commands.split()) + "\n"
    raise UIError(f"unsupported shell: {shell}")


def config_report(args: argparse.Namespace, caps: Capabilities) -> dict[str, Any]:
    return {
        "format_version": "1.0",
        "kind": "futurediff-cli-ui-config",
        "version": VERSION,
        "binary": args.binary,
        "color": caps.color,
        "unicode": caps.unicode,
        "interactive": caps.interactive,
        "json": args.json,
        "quiet": args.quiet,
        "confirmation_policy": "risky commands require YES or --yes",
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="futurediff-ui",
        description="Clean terminal interface and safe wrapper for FutureDiff.",
        epilog="Unrecognized commands are passed through to the canonical FutureDiff binary.",
    )
    parser.add_argument("--binary", default=os.getenv("FUTUREDIFF_BINARY", DEFAULT_BINARY), help="canonical FutureDiff executable")
    parser.add_argument("--json", action="store_true", help="emit machine-readable JSON for wrapper commands")
    parser.add_argument("--quiet", action="store_true", help="suppress wrapper decoration")
    parser.add_argument("--no-color", action="store_true", help="disable ANSI color")
    parser.add_argument("--force-color", action="store_true", help="enable ANSI color even when stdout is not a TTY")
    parser.add_argument("--yes", action="store_true", help="confirm risky pass-through commands")
    parser.add_argument("--version", action="version", version=f"%(prog)s {VERSION}")
    sub = parser.add_subparsers(dest="ui_command")

    doctor_p = sub.add_parser("doctor", help="check local CLI prerequisites")
    doctor_p.add_argument("--root", type=Path, help="optional FutureDiff state root")

    status_p = sub.add_parser("status", help="show production completion status")
    status_p.add_argument("--status-dir", type=Path, default=Path("dist/closure"), help="directory containing closure JSON status files")

    sub.add_parser("config", help="show resolved UI behavior")
    completion_p = sub.add_parser("completion", help="print a shell-completion script")
    completion_p.add_argument("shell", choices=("bash", "zsh", "fish"))
    completion_p.add_argument("--program", default="futurediff-ui")

    exec_p = sub.add_parser("exec", help="run the canonical FutureDiff CLI")
    exec_p.add_argument("arguments", nargs=argparse.REMAINDER)
    return parser


def _passthrough_args(argv: Sequence[str]) -> list[str]:
    passthrough: list[str] = []
    skip_next = False
    for value in argv:
        if skip_next:
            skip_next = False
            continue
        if value in {"--quiet", "--no-color", "--force-color", "--yes"}:
            continue
        if value == "--binary":
            skip_next = True
            continue
        if value.startswith("--binary="):
            continue
        passthrough.append(value)
    return passthrough


def _first_positional(argv: Sequence[str]) -> str | None:
    skip_next = False
    for value in argv:
        if skip_next:
            skip_next = False
            continue
        if value == "--binary":
            skip_next = True
            continue
        if value.startswith("--binary=") or value in {"--json", "--quiet", "--no-color", "--force-color", "--yes", "--version"}:
            continue
        if value.startswith("-"):
            continue
        return value
    return None


def _global_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--binary", default=os.getenv("FUTUREDIFF_BINARY", DEFAULT_BINARY))
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--quiet", action="store_true")
    parser.add_argument("--no-color", action="store_true")
    parser.add_argument("--force-color", action="store_true")
    parser.add_argument("--yes", action="store_true")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    wrapper_commands = {"doctor", "status", "config", "completion", "exec"}
    first = _first_positional(argv)
    if first is not None and first not in wrapper_commands:
        args, _unknown = _global_parser().parse_known_args(argv)
        caps = detect_capabilities(no_color=args.no_color, force_color=args.force_color)
        renderer = Renderer(caps, quiet=args.quiet or args.json)
        try:
            return run_command(args.binary, _passthrough_args(argv), renderer, json_mode=args.json, yes=args.yes)
        except UIError as exc:
            if args.json:
                print(json.dumps({"format_version": "1.0", "kind": "futurediff-cli-ui-error", "passed": False, "error": str(exc)}, sort_keys=True))
            else:
                renderer.status("fail", str(exc))
            return 2

    parser = build_parser()
    args = parser.parse_args(argv)
    caps = detect_capabilities(no_color=args.no_color, force_color=args.force_color)
    renderer = Renderer(caps, quiet=args.quiet or args.json)

    try:
        if args.ui_command == "doctor":
            report = doctor(args.binary, args.root)
            print(json.dumps(report, sort_keys=True)) if args.json else render_doctor(report, renderer)
            return 0 if report["passed"] else 1
        if args.ui_command == "status":
            report = status_report(args.status_dir)
            print(json.dumps(report, sort_keys=True)) if args.json else render_status(report, renderer)
            return 0 if report["production_complete"] else 4
        if args.ui_command == "config":
            report = config_report(args, caps)
            if args.json:
                print(json.dumps(report, sort_keys=True))
            else:
                renderer.heading("FutureDiff CLI UI configuration")
                renderer.table([(k, str(v)) for k, v in report.items() if k not in {"format_version", "kind"}])
            return 0
        if args.ui_command == "completion":
            print(completion_script(args.shell, args.program), end="")
            return 0
        if args.ui_command == "exec":
            command_args = list(args.arguments)
            if command_args[:1] == ["--"]:
                command_args = command_args[1:]
            if not command_args:
                raise UIError("exec requires FutureDiff command arguments")
            if args.json and "--json" not in command_args:
                command_args.insert(0, "--json")
            return run_command(args.binary, command_args, renderer, json_mode=args.json, yes=args.yes)

        passthrough = _passthrough_args(argv)
        if passthrough:
            return run_command(args.binary, passthrough, renderer, json_mode=args.json, yes=args.yes)
        parser.print_help()
        return 0
    except UIError as exc:
        if args.json:
            print(json.dumps({"format_version": "1.0", "kind": "futurediff-cli-ui-error", "passed": False, "error": str(exc)}, sort_keys=True))
        else:
            renderer.status("fail", str(exc))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
