from __future__ import annotations

import logging
import shutil
import sys
import tempfile
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.services.hooks import HookManager  # noqa: E402


class HookManagerTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-hooks-central"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        self.logger = logging.getLogger(f"central-hook-test.{self.id()}")
        self.logger.handlers.clear()
        self.logger.propagate = True
        self.logger.setLevel(logging.INFO)
        self.hook_manager = HookManager(self.temp_dir, self.logger)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_run_command_logs_successful_stdout(self) -> None:
        with self.assertLogs(self.logger.name, level="INFO") as logs:
            self.hook_manager.run_command(
                "printf hook-output",
                phase="post",
                context={},
            )

        self.assertTrue(any("hook-output" in message for message in logs.output))

    def test_save_uploaded_shell_script_normalizes_crlf_line_endings(self) -> None:
        self.hook_manager.save_uploaded_file("show_ls.sh", b"pwd\r\nls -la\r\n")

        content = (self.temp_dir / "show_ls.sh").read_bytes()
        self.assertEqual(content, b"pwd\nls -la\n")

    def test_save_uploaded_text_helper_file_is_allowed(self) -> None:
        self.hook_manager.save_uploaded_file("notes.txt", b"helper\r\nvalue\r\n")

        content = (self.temp_dir / "notes.txt").read_bytes()
        self.assertEqual(content, b"helper\r\nvalue\r\n")

    def test_save_uploaded_extensionless_file_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, r"only \.sh scripts or \.txt helper files"):
            self.hook_manager.save_uploaded_file("helper", b"value\n")


if __name__ == "__main__":
    unittest.main()
