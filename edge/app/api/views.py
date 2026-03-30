from __future__ import annotations

from pathlib import Path

from fastapi.templating import Jinja2Templates


API_DIR = Path(__file__).resolve().parent
STATIC_DIR = API_DIR / "static"
templates = Jinja2Templates(directory=str(API_DIR / "templates"))
