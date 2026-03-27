from __future__ import annotations

import time


def run_scheduler(run_once_callable, interval_seconds: int, logger) -> None:
    while True:
        started = time.monotonic()
        run_once_callable()
        elapsed = time.monotonic() - started
        sleep_seconds = max(1, interval_seconds - int(elapsed))
        logger.info("sleeping seconds=%s", sleep_seconds)
        time.sleep(sleep_seconds)

