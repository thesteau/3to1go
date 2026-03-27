from __future__ import annotations

import threading

from app.services.runner import EdgeRunner


class SchedulerController:
    def __init__(self, runner: EdgeRunner) -> None:
        self.runner = runner
        self._stop_event = threading.Event()
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        if self._thread is not None and self._thread.is_alive():
            return
        self._thread = threading.Thread(target=self._loop, name="edge-scheduler", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop_event.set()
        if self._thread is not None:
            self._thread.join(timeout=2)

    def _loop(self) -> None:
        while not self._stop_event.is_set():
            self.runner.run_cycle()
            self._stop_event.wait(self.runner.settings.interval_seconds)
