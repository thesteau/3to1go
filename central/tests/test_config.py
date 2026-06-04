from __future__ import annotations

import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.core import config  # noqa: E402


class ConfigTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-config"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def _key_path(self) -> Path:
        return self.temp_dir / "relay_issuer.key"

    def _key_env(self) -> dict[str, str]:
        return {"ISSUER_KEY_FILE": str(self._key_path())}

    def _settings_env(self) -> dict[str, str]:
        return {
            **self._key_env(),
            "POSTGRES_USER": "relay",
            "POSTGRES_PASSWORD": "secret",
        }

    def test_build_settings_defaults_http_port_to_6555(self) -> None:
        with patch.dict(os.environ, self._settings_env(), clear=True):
            settings = config.build_settings({})

        self.assertEqual(settings.http_port, 6555)

    def test_hook_scripts_dir_uses_dedicated_container_directory(self) -> None:
        with patch.dict(os.environ, {"XDG_CONFIG_HOME": "/config"}, clear=True):
            self.assertEqual(config.hook_scripts_dir(), Path("/hook-scripts"))

    def test_build_index_database_url_prefers_explicit_url(self) -> None:
        with patch.dict(
            os.environ,
            {
                "INDEX_DATABASE_URL": "postgresql://custom:secret@db:5432/customdb",
                "INDEX_DATABASE_USER": "relay",
                "INDEX_DATABASE_PASSWORD": "ignored",
            },
            clear=True,
        ):
            url = config._build_index_database_url()

        self.assertEqual(url, "postgresql://custom:secret@db:5432/customdb")

    def test_build_index_database_url_uses_simple_credentials_defaults(self) -> None:
        with patch.dict(
            os.environ,
            {
                "POSTGRES_USER": "relay",
                "POSTGRES_PASSWORD": "secret",
            },
            clear=True,
        ):
            url = config._build_index_database_url()

        self.assertEqual(url, "postgresql://relay:secret@postgres:5432/relaycentral")

    def test_build_index_database_url_prefers_index_specific_credentials_when_present(self) -> None:
        with patch.dict(
            os.environ,
            {
                "INDEX_DATABASE_USER": "relay-index",
                "INDEX_DATABASE_PASSWORD": "secret-index",
                "POSTGRES_USER": "relay",
                "POSTGRES_PASSWORD": "secret",
                "POSTGRES_DB": "relaycentral",
            },
            clear=True,
        ):
            url = config._build_index_database_url()

        self.assertEqual(url, "postgresql://relay-index:secret-index@postgres:5432/relaycentral")

    def test_build_index_database_url_requires_credentials(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(RuntimeError):
                config._build_index_database_url()

    def test_load_settings_requires_postgres_credentials(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(RuntimeError):
                config.load_settings()

    def test_build_settings_uses_ntfy_and_hook_env_defaults_when_config_is_blank(self) -> None:
        with patch.dict(
            os.environ,
            {
                **self._settings_env(),
                "NTFY_URL": "https://ntfy.example.com",
                "NTFY_TOPIC": "uploads",
                "NTFY_MESSAGE_TEMPLATE": "Hello {{ edge_id }}",
                "NTFY_MATCH_EDGE_ID": "edge-01",
                "NTFY_MATCH_EDGE_INSTANCE_ID": "edgeinstance0001",
                "NTFY_MATCH_SOURCE": "192.168.1.10",
                "HOOK_PRE_COMMAND": "pre.sh",
                "HOOK_POST_COMMAND": "post.sh",
            },
            clear=True,
        ):
            settings = config.build_settings({})

        self.assertEqual(settings.ntfy_url, "https://ntfy.example.com")
        self.assertEqual(settings.ntfy_topic, "uploads")
        self.assertEqual(settings.ntfy_message_template, "Hello {{ edge_id }}")
        self.assertEqual(settings.ntfy_match_edge_id, "edge-01")
        self.assertEqual(settings.ntfy_match_edge_instance_id, "edgeinstance0001")
        self.assertEqual(settings.ntfy_match_source, "192.168.1.10")
        self.assertEqual(settings.hook_pre_command, "pre.sh")
        self.assertEqual(settings.hook_post_command, "post.sh")

    def test_build_settings_prefers_saved_ntfy_and_hook_values_over_env_defaults(self) -> None:
        with patch.dict(
            os.environ,
            {
                **self._settings_env(),
                "NTFY_URL": "https://ntfy.example.com",
                "NTFY_TOPIC": "uploads",
                "HOOK_PRE_COMMAND": "pre.sh",
                "HOOK_POST_COMMAND": "post.sh",
            },
            clear=True,
        ):
            settings = config.build_settings(
                {
                    "ntfy_url": "https://saved.example.com",
                    "ntfy_topic": "saved-topic",
                    "hook_pre_command": "saved-pre.sh",
                    "hook_post_command": "saved-post.sh",
                }
            )

        self.assertEqual(settings.ntfy_url, "https://saved.example.com")
        self.assertEqual(settings.ntfy_topic, "saved-topic")
        self.assertEqual(settings.hook_pre_command, "saved-pre.sh")
        self.assertEqual(settings.hook_post_command, "saved-post.sh")


if __name__ == "__main__":
    unittest.main()
