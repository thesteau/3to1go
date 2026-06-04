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

from app.services.certificates import CertificateManager  # noqa: E402


CERT_TEXT = """-----BEGIN CERTIFICATE-----
MIIBtest
-----END CERTIFICATE-----
"""


class CertificateManagerTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-certificates-edge"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        self.storage_dir = self.temp_dir / "stored"
        self.trust_dir = self.temp_dir / "trust"
        self.marker = self.temp_dir / "updated"
        self.manager = CertificateManager(
            storage_dir=self.storage_dir,
            trust_target_dir=self.trust_dir,
            update_command=f"touch {self.marker}",
        )

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_empty_certificate_directory_is_allowed(self) -> None:
        self.assertEqual(self.manager.list_files(), [])
        self.assertFalse(self.marker.exists())

    def test_save_certificate_installs_to_trust_store_and_updates_trust(self) -> None:
        saved = self.manager.save_uploaded_file("home-ca.crt", CERT_TEXT.encode("utf-8"))

        self.assertEqual(saved["name"], "home-ca.crt")
        self.assertEqual((self.storage_dir / "home-ca.crt").read_text(encoding="utf-8"), CERT_TEXT)
        self.assertEqual((self.trust_dir / "home-ca.crt").read_text(encoding="utf-8"), CERT_TEXT)
        self.assertTrue(self.marker.exists())

    def test_save_certificate_rejects_non_crt_extension(self) -> None:
        with self.assertRaisesRegex(ValueError, "only .crt certificate files are allowed"):
            self.manager.save_uploaded_file("home-ca.pem", CERT_TEXT.encode("utf-8"))

    def test_save_certificate_rejects_content_without_certificate_markers(self) -> None:
        with self.assertRaisesRegex(ValueError, "certificate must be PEM encoded"):
            self.manager.save_uploaded_file("home-ca.crt", b"not a certificate")

    def test_delete_certificate_removes_storage_and_trust_copy_then_updates_trust(self) -> None:
        self.manager.save_uploaded_file("../home-ca.crt", CERT_TEXT.encode("utf-8"))
        self.marker.unlink()

        self.manager.delete_file("home-ca.crt")

        self.assertFalse((self.storage_dir / "home-ca.crt").exists())
        self.assertFalse((self.trust_dir / "home-ca.crt").exists())
        self.assertTrue(self.marker.exists())


if __name__ == "__main__":
    unittest.main()
