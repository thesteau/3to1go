from __future__ import annotations

import sys
from pathlib import Path

from fastapi.templating import Jinja2Templates


if getattr(sys, "frozen", False) and hasattr(sys, "_MEIPASS"):
    API_DIR = Path(sys._MEIPASS) / "app" / "api"
else:
    API_DIR = Path(__file__).resolve().parent

STATIC_DIR = API_DIR / "static"
templates = Jinja2Templates(directory=str(API_DIR / "templates"))
