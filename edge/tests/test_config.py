from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.api.app import create_app  # noqa: E402
from app.core import config  # noqa: E402
from app.services.state import JobState, StateStore  # noqa: E402
from app.services.user_store import UserStore  # noqa: E402


class ConfigTests(unittest.TestCase):
    def test_build_settings_defaults_cron_schedule_to_sunday_2am(self) -> None:
        settings = config.build_settings({})
        self.assertEqual(settings.cron_schedule, "0 2 * * 0")

    def test_build_settings_defaults_ports_to_edge_6556_and_central_6555(self) -> None:
        settings = config.build_settings({})
        self.assertEqual(settings.http_port, 6556)
        self.assertEqual(settings.central_url, "http://127.0.0.1:6555")

    def test_build_settings_uses_docker_friendly_defaults_in_container_layout(self) -> None:
        with patch.dict(os.environ, {"XDG_CONFIG_HOME": "/config"}, clear=True):
            with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
                settings = config.build_settings({})

        self.assertTrue(settings.scan_root.as_posix().endswith("/scan"))
        self.assertEqual(settings.http_host, "0.0.0.0")

    def test_build_settings_preserves_explicit_state_paths(self) -> None:
        with patch.dict(
            os.environ,
            {
                "XDG_CONFIG_HOME": "/config",
                "XDG_STATE_HOME": "/data/state",
                "XDG_CACHE_HOME": "/data/cache",
            },
            clear=True,
        ):
            settings = config.build_settings(
                {
                    "state_dir": "/custom/state",
                    "spool_dir": "/custom/spool",
                }
            )

        self.assertEqual(settings.state_dir, Path("/custom/state"))
        self.assertEqual(settings.spool_dir, Path("/custom/spool"))

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

    def test_build_settings_uses_ntfy_and_hook_values_from_payload(self) -> None:
        with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
            settings = config.build_settings(
                {
                    "ntfy_url": "https://saved.example.com",
                    "ntfy_topic": "saved-topic",
                    "ntfy_message_template": "Edge {{ edge_id }}",
                    "hook_pre_command": "saved-pre.sh",
                    "hook_post_command": "saved-post.sh",
                }
            )

        self.assertEqual(settings.ntfy_url, "https://saved.example.com")
        self.assertEqual(settings.ntfy_topic, "saved-topic")
        self.assertEqual(settings.ntfy_message_template, "Edge {{ edge_id }}")
        self.assertEqual(settings.hook_pre_command, "saved-pre.sh")
        self.assertEqual(settings.hook_post_command, "saved-post.sh")

    def test_create_app_initializes_new_routes_without_starting_scheduler(self) -> None:
        with patch.object(Path, "home", return_value=Path("/tmp/relay-home")):
            app = create_app(
                start_scheduler=False,
                user_store_path=PROJECT_ROOT / ".tmp-test-config" / "edge-users-app.db",
            )

        self.assertEqual(app.title, "RelayCentralizer Edge")

    def test_state_store_recreates_missing_state_dir_before_save(self) -> None:
        state_dir = PROJECT_ROOT / ".tmp-test-config" / "missing-state-dir"
        store = StateStore(state_dir)
        state_dir.rmdir()

        store.set("job", JobState(job_name="job", active_phase="scanning"))

        self.assertTrue((state_dir / "edge-state.json").exists())

    def test_user_store_persists_password_change_across_instances(self) -> None:
        db_path = PROJECT_ROOT / ".tmp-test-config" / "edge-users.db"
        db_path.unlink(missing_ok=True)
        first_store = UserStore(sqlite_path=db_path)
        admin = first_store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)
        first_store.change_password(
            admin["id"],
            current_password="admin",
            new_password="changed-admin",
        )

        second_store = UserStore(sqlite_path=db_path)
        self.assertIsNone(second_store.authenticate("admin", "admin"))
        changed = second_store.authenticate("admin", "changed-admin")
        self.assertIsNotNone(changed)
        self.assertFalse(changed["must_change_password"])

    def test_user_store_rejects_wrong_current_password(self) -> None:
        db_path = PROJECT_ROOT / ".tmp-test-config" / "edge-users-current-password.db"
        db_path.unlink(missing_ok=True)
        store = UserStore(sqlite_path=db_path)
        admin = store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)

        with self.assertRaises(ValueError):
            store.change_password(admin["id"], current_password="wrong", new_password="changed-admin")

    def test_user_store_rejects_short_or_space_only_passwords(self) -> None:
        db_path = PROJECT_ROOT / ".tmp-test-config" / "edge-users-password-policy.db"
        db_path.unlink(missing_ok=True)
        store = UserStore(sqlite_path=db_path)

        with self.assertRaises(ValueError):
            store.create_user("short", "abcd")
        with self.assertRaises(ValueError):
            store.create_user("blank", "     ")
        created = store.create_user("words", "words")

        self.assertEqual(created["username"], "words")

    def test_user_store_always_requires_change_for_default_password(self) -> None:
        db_path = PROJECT_ROOT / ".tmp-test-config" / "edge-users-default-password.db"
        db_path.unlink(missing_ok=True)
        store = UserStore(sqlite_path=db_path)
        admin = store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)
        updated = store.update_user(admin["id"], password="admin", must_change_password=False)
        self.assertTrue(updated["must_change_password"])

        logged_in = store.authenticate("admin", "admin")

        self.assertIsNotNone(logged_in)
        self.assertTrue(logged_in["must_change_password"])

    def test_user_store_keeps_bootstrap_user_admin_after_rename(self) -> None:
        db_path = PROJECT_ROOT / ".tmp-test-config" / "edge-users-bootstrap.db"
        db_path.unlink(missing_ok=True)
        store = UserStore(sqlite_path=db_path)
        admin = store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)

        renamed = store.update_user(admin["id"], username="owner", is_admin=False)

        self.assertEqual(renamed["username"], "owner")
        self.assertTrue(renamed["is_admin"])
        self.assertTrue(renamed["is_bootstrap_admin"])
        with self.assertRaises(ValueError):
            store.delete_user(admin["id"])


if __name__ == "__main__":
    unittest.main()
