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
    def test_build_settings_defaults_cron_schedule_to_sunday_2am(self) -> None:
        settings = config.build_settings({})
        self.assertEqual(settings.cron_schedule, "0 2 * * 0")

    def test_build_settings_defaults_ports_to_edge_6556_and_central_6555(self) -> None:
        settings = config.build_settings({})
        self.assertEqual(settings.http_port, 6556)
        self.assertEqual(settings.central_url, "http://127.0.0.1:6555")
        self.assertEqual(settings.advertised_url, "")

    def test_build_settings_uses_docker_friendly_defaults_in_container_layout(self) -> None:
        with patch.dict(os.environ, {"XDG_CONFIG_HOME": "/config"}, clear=True):
            with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
                settings = config.build_settings({})

        self.assertTrue(settings.scan_root.as_posix().endswith("/scan"))
        self.assertEqual(settings.http_host, "0.0.0.0")

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

    def test_load_settings_applies_upload_and_circuit_breaker_env_overrides(self) -> None:
        with patch.dict(
            os.environ,
            {
                "UPLOAD_RETRY_MAX_ATTEMPTS": "7",
                "UPLOAD_RETRY_BASE_DELAY_SECONDS": "11",
                "CIRCUIT_BREAKER_FAILURE_THRESHOLD": "3",
                "CIRCUIT_BREAKER_COOLDOWN_SECONDS": "45",
            },
            clear=True,
        ):
            with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
                settings = config.load_settings()

        self.assertEqual(settings.upload_retry_max_attempts, 7)
        self.assertEqual(settings.upload_retry_base_delay_seconds, 11)
        self.assertEqual(settings.circuit_breaker_failure_threshold, 3)
        self.assertEqual(settings.circuit_breaker_cooldown_seconds, 45)

    def test_load_settings_ignores_cron_schedule_env_override(self) -> None:
        with patch.dict(os.environ, {"CRON_SCHEDULE": "0 5 * * *"}, clear=True):
            with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
                settings = config.load_settings()

        self.assertEqual(settings.cron_schedule, "0 2 * * 0")


if __name__ == "__main__":
    unittest.main()
