from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path


EDGE_DIR = Path(__file__).resolve().parents[1]
VENV_DIR = EDGE_DIR / ".venv"
RUNTIME_DIR = EDGE_DIR / ".dev-runtime"
PID_FILE = RUNTIME_DIR / "edge.pid"
LOG_FILE = RUNTIME_DIR / "edge.log"
STAMP_FILE = RUNTIME_DIR / "requirements.sha256"
REQUIREMENTS_FILE = EDGE_DIR / "requirements.txt"

COMMAND_DESCRIPTIONS: dict[str, str] = {
    "setup": "Create the local .venv and install or refresh Python dependencies.",
    "run": "Start Edge in the foreground using the managed virtual environment.",
    "start": "Start Edge in the background and write logs to .dev-runtime/edge.log.",
    "stop": "Stop the background Edge process started by this dev runner.",
    "restart": "Restart the background Edge process.",
    "status": "Show the virtual environment, log path, and background process status.",
    "clean": "Remove .venv and .dev-runtime after Edge has been stopped.",
}


def _venv_python() -> Path:
    if os.name == "nt":
        return VENV_DIR / "Scripts" / "python.exe"
    return VENV_DIR / "bin" / "python"


def _requirements_hash() -> str:
    return hashlib.sha256(REQUIREMENTS_FILE.read_bytes()).hexdigest()


def _read_stamp() -> str | None:
    try:
        return STAMP_FILE.read_text(encoding="utf-8").strip()
    except OSError:
        return None


def _write_stamp(value: str) -> None:
    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    STAMP_FILE.write_text(value, encoding="utf-8")


def _read_pid() -> int | None:
    try:
        return int(PID_FILE.read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return None


def _write_pid(pid: int) -> None:
    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    PID_FILE.write_text(str(pid), encoding="utf-8")


def _clear_pid() -> None:
    PID_FILE.unlink(missing_ok=True)


def _is_running(pid: int | None) -> bool:
    if pid is None:
        return False
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _ensure_venv() -> Path:
    python_path = _venv_python()
    if not python_path.exists():
        print(f"Creating virtual environment at {VENV_DIR}")
        subprocess.check_call([sys.executable, "-m", "venv", str(VENV_DIR)], cwd=EDGE_DIR)
    return python_path


def _ensure_requirements() -> Path:
    python_path = _ensure_venv()
    required_hash = _requirements_hash()
    if _read_stamp() == required_hash:
        return python_path

    print("Installing Edge development dependencies")
    subprocess.check_call([str(python_path), "-m", "pip", "install", "-r", str(REQUIREMENTS_FILE)], cwd=EDGE_DIR)
    _write_stamp(required_hash)
    return python_path


def setup() -> int:
    python_path = _ensure_requirements()
    print(f"Edge dev environment ready: {python_path}")
    return 0


def run_foreground() -> int:
    python_path = _ensure_requirements()
    print("Starting Edge in the foreground")
    return subprocess.call([str(python_path), "-m", "app.main"], cwd=EDGE_DIR)


def start_background() -> int:
    pid = _read_pid()
    if _is_running(pid):
        print(f"Edge is already running in the background with PID {pid}")
        print(f"Log file: {LOG_FILE}")
        return 0

    python_path = _ensure_requirements()
    RUNTIME_DIR.mkdir(parents=True, exist_ok=True)
    log_handle = LOG_FILE.open("a", encoding="utf-8")
    log_handle.write("\n=== starting edge dev process ===\n")
    log_handle.flush()

    kwargs: dict[str, object] = {
        "cwd": EDGE_DIR,
        "stdout": log_handle,
        "stderr": subprocess.STDOUT,
    }
    if os.name == "nt":
        kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.DETACHED_PROCESS
    else:
        kwargs["start_new_session"] = True

    process = subprocess.Popen([str(python_path), "-m", "app.main"], **kwargs)
    _write_pid(process.pid)
    print(f"Edge started in the background with PID {process.pid}")
    print(f"Log file: {LOG_FILE}")
    return 0


def stop_background() -> int:
    pid = _read_pid()
    if not _is_running(pid):
        _clear_pid()
        print("Edge is not running")
        return 0

    assert pid is not None
    print(f"Stopping Edge process {pid}")
    if os.name == "nt":
        result = subprocess.run(
            ["taskkill", "/PID", str(pid), "/T", "/F"],
            cwd=EDGE_DIR,
            check=False,
            capture_output=True,
            text=True,
        )
        if result.returncode not in {0, 128}:
            sys.stderr.write(result.stdout)
            sys.stderr.write(result.stderr)
            return result.returncode
    else:
        os.kill(pid, signal.SIGTERM)
        for _ in range(50):
            if not _is_running(pid):
                break
            time.sleep(0.1)
        if _is_running(pid):
            os.kill(pid, signal.SIGKILL)

    _clear_pid()
    print("Edge stopped")
    return 0


def status() -> int:
    pid = _read_pid()
    python_path = _venv_python()
    print(f"Edge directory: {EDGE_DIR}")
    print(f"Virtual environment: {VENV_DIR}")
    print(f"Virtual environment python: {python_path}")
    print(f"Log file: {LOG_FILE}")
    print(f"Requirements installed: {'yes' if _read_stamp() == _requirements_hash() else 'no'}")
    if _is_running(pid):
        print(f"Background process: running (PID {pid})")
    else:
        print("Background process: stopped")
    return 0


def restart() -> int:
    stop_background()
    return start_background()


def clean() -> int:
    if _is_running(_read_pid()):
        print("Stop Edge before cleaning the development environment")
        return 1
    shutil.rmtree(VENV_DIR, ignore_errors=True)
    shutil.rmtree(RUNTIME_DIR, ignore_errors=True)
    print("Removed Edge development virtual environment and runtime files")
    return 0


def print_overview() -> None:
    print("RelayCentralizer Edge development runner")
    print("")
    print("Commands:")
    for name, description in COMMAND_DESCRIPTIONS.items():
        print(f"  {name:<8} {description}")
    print("")
    print("Examples:")
    print("  python edge/scripts/dev_edge.py setup")
    print("  python edge/scripts/dev_edge.py start")
    print("  python edge/scripts/dev_edge.py status")
    print("  python edge/scripts/dev_edge.py stop")
    print("")
    print("Tip: use --help for argparse help output.")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Manage the Edge development environment",
        epilog="\n".join(
            [
                "Commands:",
                *[f"  {name:<8} {description}" for name, description in COMMAND_DESCRIPTIONS.items()],
            ]
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "command",
        nargs="?",
        choices=list(COMMAND_DESCRIPTIONS),
        help="Command to run. Omit it to print a short overview.",
    )
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    if not args.command:
        print_overview()
        return 0

    if args.command == "setup":
        return setup()
    if args.command == "run":
        return run_foreground()
    if args.command == "start":
        return start_background()
    if args.command == "stop":
        return stop_background()
    if args.command == "restart":
        return restart()
    if args.command == "status":
        return status()
    if args.command == "clean":
        return clean()
    parser.error(f"unknown command: {args.command}")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
