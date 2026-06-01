from __future__ import annotations

import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


MAX_HOOK_FILES = 3
_ALLOWED_SUFFIXES = {"", ".sh", ".txt"}
_HOOK_TIMEOUT_SECONDS = 300


class HookManager:
    def __init__(self, scripts_dir: Path, logger) -> None:
        self.scripts_dir = scripts_dir
        self.logger = logger
        self.scripts_dir.mkdir(parents=True, exist_ok=True)

    def snapshot(self, *, pre_command: str, post_command: str) -> dict[str, Any]:
        return {
            "pre_command": pre_command,
            "post_command": post_command,
            "script_dir": str(self.scripts_dir),
            "max_files": MAX_HOOK_FILES,
            "files": self.list_files(),
        }

    def list_files(self) -> list[dict[str, Any]]:
        self.scripts_dir.mkdir(parents=True, exist_ok=True)
        files = sorted((path for path in self.scripts_dir.iterdir() if path.is_file()), key=lambda path: path.name.lower())
        return [self._file_payload(path) for path in files[:MAX_HOOK_FILES]]

    def save_uploaded_file(self, filename: str, content: bytes) -> dict[str, Any]:
        safe_name = self._sanitize_filename(filename)
        suffix = Path(safe_name).suffix.lower()
        if suffix not in _ALLOWED_SUFFIXES:
            raise ValueError("only .sh, .txt, or extensionless text files are allowed")

        try:
            text = content.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise ValueError("only UTF-8 text files are allowed") from exc

        existing = {path.name for path in self._all_files()}
        if safe_name not in existing and len(existing) >= MAX_HOOK_FILES:
            raise ValueError("only the first 3 files are supported here")

        target = self.scripts_dir / safe_name
        target.write_text(text, encoding="utf-8")
        if os.name != "nt" and suffix == ".sh":
            current_mode = target.stat().st_mode
            target.chmod(current_mode | 0o700)
        return self._file_payload(target)

    def read_text_file(self, filename: str) -> dict[str, str]:
        path = self._resolve_file(filename)
        try:
            content = path.read_text(encoding="utf-8")
        except UnicodeDecodeError as exc:
            raise ValueError("this file cannot be viewed because it is not text") from exc
        return {"filename": path.name, "content": content}

    def delete_file(self, filename: str) -> None:
        path = self._resolve_file(filename)
        path.unlink(missing_ok=True)

    def run_command(self, command: str, *, phase: str, context: dict[str, Any]) -> None:
        normalized = str(command or "").strip()
        if not normalized:
            return

        shell_command = self._resolve_command(normalized)
        env = os.environ.copy()
        env.update(self._build_env(phase, context))

        try:
            result = subprocess.run(
                shell_command,
                shell=True,
                cwd=self.scripts_dir,
                env=env,
                capture_output=True,
                text=True,
                timeout=_HOOK_TIMEOUT_SECONDS,
            )
        except Exception as exc:
            self.logger.warning("hook_execution_failed phase=%s command=%s detail=%s", phase, normalized, exc)
            return

        if result.returncode != 0:
            detail = (result.stderr or result.stdout or "").strip()
            self.logger.warning(
                "hook_execution_nonzero phase=%s command=%s code=%s detail=%s",
                phase,
                normalized,
                result.returncode,
                detail,
            )

    def _all_files(self) -> list[Path]:
        self.scripts_dir.mkdir(parents=True, exist_ok=True)
        return sorted((path for path in self.scripts_dir.iterdir() if path.is_file()), key=lambda path: path.name.lower())

    def _resolve_command(self, command: str) -> str:
        stripped = command.strip()
        if any(char.isspace() for char in stripped):
            return stripped
        candidate = self.scripts_dir / stripped
        if candidate.exists() and candidate.is_file():
            prefix = ".\\" if os.name == "nt" else "./"
            return f"{prefix}{candidate.name}"
        return stripped

    def _build_env(self, phase: str, context: dict[str, Any]) -> dict[str, str]:
        env = {
            "RELAY_APP": "central",
            "RELAY_HOOK_PHASE": phase,
            "RELAY_HOOK_SCRIPTS_DIR": str(self.scripts_dir),
        }
        for key, value in context.items():
            env_key = f"RELAY_{key.upper()}"
            env[env_key] = "" if value is None else str(value)
        return env

    def _resolve_file(self, filename: str) -> Path:
        safe_name = self._sanitize_filename(filename)
        path = self.scripts_dir / safe_name
        if not path.exists() or not path.is_file():
            raise FileNotFoundError(safe_name)
        return path

    def _sanitize_filename(self, filename: str) -> str:
        safe_name = Path(str(filename or "").strip()).name
        if not safe_name or safe_name in {".", ".."}:
            raise ValueError("filename is required")
        return safe_name

    def _file_payload(self, path: Path) -> dict[str, Any]:
        try:
            viewable = True
            path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            viewable = False

        return {
            "name": path.name,
            "size_bytes": path.stat().st_size,
            "modified_at": datetime.fromtimestamp(path.stat().st_mtime, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "viewable": viewable,
        }
