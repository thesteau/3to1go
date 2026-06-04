from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
ENTRYPOINT = PROJECT_ROOT / "docker-entrypoint.sh"
WORKSPACE_ROOT = PROJECT_ROOT.parent


class DockerEntrypointTests(unittest.TestCase):
    def setUp(self) -> None:
        temp_root = WORKSPACE_ROOT / ".tmp-test-entrypoint-central"
        temp_root.mkdir(parents=True, exist_ok=True)
        self.temp_dir = Path(tempfile.mkdtemp(dir=temp_root))
        self.ca_dir = self.temp_dir / "input-certs"
        self.persisted_ca_dir = self.temp_dir / "persisted-certs"
        self.trust_dir = self.temp_dir / "trust"
        self.marker = self.temp_dir / "updated"
        self.ca_dir.mkdir()
        self.persisted_ca_dir.mkdir()
        self.trust_dir.mkdir()

    def tearDown(self) -> None:
        shutil.rmtree(self.temp_dir, ignore_errors=True)

    def test_runs_command_without_certificates(self) -> None:
        result = self._run_entrypoint("printf", "started")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.endswith("started"), result.stdout)
        self.assertFalse(self.marker.exists())

    def test_installs_mounted_crt_files_before_running_command(self) -> None:
        (self.ca_dir / "home-ca.crt").write_text("test certificate\n", encoding="utf-8")
        (self.ca_dir / "notes.txt").write_text("ignored\n", encoding="utf-8")

        result = self._run_entrypoint("printf", "started")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.endswith("started"), result.stdout)
        self.assertEqual((self.trust_dir / "home-ca.crt").read_text(encoding="utf-8"), "test certificate\n")
        self.assertTrue(self.marker.exists())

    def test_installs_persisted_uploaded_crt_files_before_running_command(self) -> None:
        (self.persisted_ca_dir / "uploaded-ca.crt").write_text("uploaded certificate\n", encoding="utf-8")

        result = self._run_entrypoint("printf", "started")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.endswith("started"), result.stdout)
        self.assertEqual((self.trust_dir / "uploaded-ca.crt").read_text(encoding="utf-8"), "uploaded certificate\n")
        self.assertTrue(self.marker.exists())

    def _run_entrypoint(self, *command: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update(
            {
                "RELAY_EXTRA_CA_DIR": str(self.ca_dir),
                "RELAY_PERSISTED_CA_DIR": str(self.persisted_ca_dir),
                "RELAY_TRUST_TARGET_DIR": str(self.trust_dir),
                "RELAY_UPDATE_CA_CERTIFICATES": f"touch {self.marker}",
            }
        )
        return subprocess.run(
            [str(ENTRYPOINT), *command],
            cwd=PROJECT_ROOT,
            env=env,
            capture_output=True,
            text=True,
            timeout=10,
        )


if __name__ == "__main__":
    unittest.main()
