from __future__ import annotations

import argparse

from app.api.app import create_app
from app.core.config import load_settings


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="RelayCentralizer Edge")
    parser.add_argument("--once", action="store_true", help="Run one scan/upload cycle and exit")
    parser.add_argument("--dry-run", action="store_true", help="Discover and fingerprint jobs without archiving or uploading")
    parser.add_argument("--no-scheduler", action="store_true", help="Serve the UI without the background scheduler")
    return parser.parse_args()


app = create_app()


def main() -> None:
    args = parse_args()
    settings = load_settings()

    if args.once:
        from app.services.runner import EdgeRunner

        EdgeRunner(settings=settings, dry_run=args.dry_run).run_cycle()
        return

    import uvicorn

    uvicorn.run(
        create_app(settings=settings, dry_run=args.dry_run, start_scheduler=not args.no_scheduler),
        host=settings.http_host,
        port=settings.http_port,
    )


if __name__ == "__main__":
    main()
