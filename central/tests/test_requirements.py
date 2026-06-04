from __future__ import annotations

import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]


class RequirementsTests(unittest.TestCase):
    def test_central_requirements_include_signing_dependencies(self) -> None:
        requirements = (PROJECT_ROOT / "requirements.txt").read_text()

        self.assertIn("cryptography==", requirements)


if __name__ == "__main__":
    unittest.main()
