from __future__ import annotations

import threading
from datetime import datetime, timedelta
from typing import Any

from app.core.schedule import CronSchedule, MINIMUM_SCHEDULE_MINUTES
from app.services.runner import EdgeRunner


_MINIMUM_CYCLE_GAP = timedelta(minutes=MINIMUM_SCHEDULE_MINUTES)


class SchedulerController:
    def __init__(self, runner: EdgeRunner) -> None:
        self.runner = runner
        self.schedule = CronSchedule.from_expression(runner.settings.cron_schedule)
        self._stop_event = threading.Event()
        self._wake_event = threading.Event()
        self._thread: threading.Thread | None = None
        self._status_lock = threading.Lock()
        self._state = "idle"
        self._next_run_at: datetime | None = None
        self._last_started_at: datetime | None = None
        self._last_completed_at: datetime | None = None
        self._run_now_requested = True

    def start(self) -> None:
        if self._thread is not None and self._thread.is_alive():
            return
        self._thread = threading.Thread(target=self._loop, name="edge-scheduler", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop_event.set()
        self._wake_event.set()
        if self._thread is not None:
            self._thread.join(timeout=5)
        with self._status_lock:
            self._state = "stopped"
            self._next_run_at = None

    def request_run_now(self) -> str:
        with self._status_lock:
            if self._state == "running":
                return "already_running"
            if self._run_now_requested:
                return "queued"
            self._run_now_requested = True
        self._wake_event.set()
        return "queued"

    def snapshot(self) -> dict[str, Any]:
        with self._status_lock:
            return {
                "state": self._state,
                "cron_schedule": self.schedule.expression,
                "minimum_cycle_gap_minutes": MINIMUM_SCHEDULE_MINUTES,
                "next_run_at": _format_datetime(self._next_run_at),
                "last_started_at": _format_datetime(self._last_started_at),
                "last_completed_at": _format_datetime(self._last_completed_at),
                "run_now_requested": self._run_now_requested,
            }

    def reload_settings(self) -> None:
        schedule = CronSchedule.from_expression(self.runner.settings.cron_schedule)
        with self._status_lock:
            self.schedule = schedule
            if self._state != "running":
                self._next_run_at = None
        self._wake_event.set()

    def _loop(self) -> None:
        while not self._stop_event.is_set():
            if self._consume_run_request():
                self._run_cycle(trigger="startup" if self._last_completed_at is None else "manual")
                continue

            next_run_at = self._compute_next_run_at()
            with self._status_lock:
                self._state = "waiting"
                self._next_run_at = next_run_at

            timeout_seconds = max(0.0, (next_run_at - self._now()).total_seconds())
            woke_early = self._wake_event.wait(timeout_seconds)
            self._wake_event.clear()
            if woke_early:
                continue

            self._run_cycle(trigger="scheduled")

    def _consume_run_request(self) -> bool:
        with self._status_lock:
            if not self._run_now_requested:
                return False
            self._run_now_requested = False
            self._next_run_at = None
            return True

    def _compute_next_run_at(self) -> datetime:
        baseline = self._last_completed_at or self._now()
        scheduled_at = self.schedule.next_after(baseline)
        if self._last_completed_at is None:
            return scheduled_at
        return max(scheduled_at, self._last_completed_at + _MINIMUM_CYCLE_GAP)

    def _run_cycle(self, trigger: str) -> None:
        started_at = self._now()
        with self._status_lock:
            self._state = "running"
            self._next_run_at = None
            self._last_started_at = started_at

        self.runner.logger.info("cycle_started trigger=%s schedule=%s", trigger, self.schedule.expression)
        try:
            self.runner.run_cycle()
        finally:
            completed_at = self._now()
            with self._status_lock:
                self._state = "idle"
                self._last_completed_at = completed_at
            self.runner.logger.info("cycle_completed trigger=%s", trigger)

    @staticmethod
    def _now() -> datetime:
        return datetime.now().astimezone()



def _format_datetime(value: datetime | None) -> str | None:
    if value is None:
        return None
    return value.isoformat(timespec="seconds")
