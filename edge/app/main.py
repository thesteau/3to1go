from __future__ import annotations

from app.api.app import create_app
from app.core.config import load_settings


settings = load_settings()
app = create_app(settings=settings)



def main() -> None:
    import uvicorn

    uvicorn.run(app, host=settings.http_host, port=settings.http_port)


if __name__ == "__main__":
    main()
