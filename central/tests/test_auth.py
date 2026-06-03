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

from app.services.user_store import UserStore  # noqa: E402
from app.core.signing import (  # noqa: E402
    load_or_create_issuer_keypair,
    mint_credential,
    public_key_to_bytes,
    verify_credential,
)


class IssuerKeypairTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-auth"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_creates_key_file_when_missing(self) -> None:
        key_path = self.temp_dir / "secrets" / "relay_issuer.key"

        private_key, public_key = load_or_create_issuer_keypair(key_path)

        self.assertTrue(key_path.exists())
        self.assertEqual(len(key_path.read_bytes()), 32)
        self.assertIsNotNone(private_key)
        self.assertIsNotNone(public_key)

    def test_reloads_existing_key_file(self) -> None:
        key_path = self.temp_dir / "relay_issuer.key"
        _, pub1 = load_or_create_issuer_keypair(key_path)
        _, pub2 = load_or_create_issuer_keypair(key_path)

        self.assertEqual(public_key_to_bytes(pub1), public_key_to_bytes(pub2))

    def test_rejects_directory_path_with_clear_error(self) -> None:
        key_dir = self.temp_dir / "relay_issuer.key"
        key_dir.mkdir()

        with self.assertRaises(RuntimeError) as ctx:
            load_or_create_issuer_keypair(key_dir)

        self.assertIn("ISSUER_KEY_FILE must point to a file", str(ctx.exception))


class CredentialTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-auth"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        key_path = self.temp_dir / "issuer.key"
        self.private_key, self.public_key = load_or_create_issuer_keypair(key_path)

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_minted_credential_verifies(self) -> None:
        credential = mint_credential(self.private_key)
        verify_credential(credential, self.public_key, frozenset())

    def test_tampered_credential_fails(self) -> None:
        credential = mint_credential(self.private_key)
        parts = credential.split(".")
        parts[1] = parts[1][:-2] + "AA"
        tampered = ".".join(parts)

        with self.assertRaises(ValueError):
            verify_credential(tampered, self.public_key, frozenset())

    def test_revoked_credential_fails(self) -> None:
        import base64
        import json

        credential = mint_credential(self.private_key)
        payload_b64 = credential.split(".")[1]
        padding = 4 - len(payload_b64) % 4
        if padding != 4:
            payload_b64 += "=" * padding
        jti = json.loads(base64.urlsafe_b64decode(payload_b64))["jti"]

        with self.assertRaises(ValueError, msg="credential revoked"):
            verify_credential(credential, self.public_key, frozenset({jti}))

    def test_wrong_key_fails(self) -> None:
        other_key_path = self.temp_dir / "other.key"
        other_private, other_public = load_or_create_issuer_keypair(other_key_path)
        credential = mint_credential(self.private_key)

        with self.assertRaises(ValueError):
            verify_credential(credential, other_public, frozenset())

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

    def test_user_store_rejects_wrong_current_password(self) -> None:
        store = UserStore(sqlite_path=self.temp_dir / "central-current-password.db")
        admin = store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)

        with self.assertRaises(ValueError):
            store.change_password(admin["id"], current_password="wrong", new_password="changed-admin")

    def test_user_store_rejects_short_or_space_only_passwords(self) -> None:
        store = UserStore(sqlite_path=self.temp_dir / "central-password-policy.db")

        with self.assertRaises(ValueError):
            store.create_user("short", "abcd")
        with self.assertRaises(ValueError):
            store.create_user("blank", "     ")
        created = store.create_user("words", "words")

        self.assertEqual(created["username"], "words")

    def test_user_store_always_requires_change_for_default_password(self) -> None:
        store = UserStore(sqlite_path=self.temp_dir / "central-default-password.db")
        admin = store.authenticate("admin", "admin")
        self.assertIsNotNone(admin)
        updated = store.update_user(admin["id"], password="admin", must_change_password=False)
        self.assertTrue(updated["must_change_password"])

        logged_in = store.authenticate("admin", "admin")

        self.assertIsNotNone(logged_in)
        self.assertTrue(logged_in["must_change_password"])


if __name__ == "__main__":
    unittest.main()
