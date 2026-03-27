from __future__ import annotations

import uvicorn

from app.api.app import create_app
from app.core.config import load_settings


app = create_app()


def main() -> None:
    settings = load_settings()
    uvicorn.run("app.main:app", host=settings.http_host, port=settings.http_port)


if __name__ == "__main__":
    main()
