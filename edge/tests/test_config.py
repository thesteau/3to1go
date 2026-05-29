from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.core import config  # noqa: E402


class ConfigTests(unittest.TestCase):
    def test_resolve_auth_token_file_path_uses_secret_dir_for_bare_filename(self) -> None:
        resolved = config._resolve_auth_token_file_path("relay_auth_token")
        self.assertEqual(resolved, Path("/run/secrets") / "relay_auth_token")

    def test_resolve_auth_token_file_path_preserves_explicit_path(self) -> None:
        resolved = config._resolve_auth_token_file_path("/tmp/relay_auth_token")
        self.assertEqual(resolved, Path("/tmp/relay_auth_token"))

    def test_env_overrides_reads_bare_filename_from_secret_dir(self) -> None:
        with patch.dict(os.environ, {"AUTH_TOKEN_FILE": "relay_auth_token"}, clear=True):
            with patch.object(Path, "read_text", autospec=True, return_value="secret\n") as read_text:
                overrides = config._env_overrides()

        self.assertEqual(overrides["auth_token"], "secret")
        read_text.assert_called_once_with(Path("/run/secrets/relay_auth_token"), encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
