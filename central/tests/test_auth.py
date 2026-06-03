from __future__ import annotations

import shutil
import sys
import tempfile
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
WORKSPACE_ROOT = PROJECT_ROOT.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from app.core.auth import _load_or_create_auth_token  # noqa: E402
from app.services.user_store import UserStore  # noqa: E402


class AuthTokenTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-auth"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_creates_token_file_when_missing(self) -> None:
        token_path = self.temp_dir / "secrets" / "relay_auth_token"

        token = _load_or_create_auth_token(token_path)

        self.assertTrue(token_path.exists())
        self.assertTrue(token)
        self.assertEqual(token_path.read_text(encoding="utf-8").strip(), token)

    def test_rejects_directory_path_with_clear_error(self) -> None:
        token_dir = self.temp_dir / "relay_auth_token"
        token_dir.mkdir()

        with self.assertRaises(RuntimeError) as context:
            _load_or_create_auth_token(token_dir)

        self.assertIn("AUTH_TOKEN_FILE must point to a file", str(context.exception))
        self.assertIn(str(token_dir), str(context.exception))

    def test_user_store_keeps_bootstrap_user_admin_after_rename(self) -> None:
        store = UserStore(sqlite_path=self.temp_dir / "central-users.db")
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
