from __future__ import annotations

import threading


class JobLockManager:
    def __init__(self) -> None:
        self._locks: dict[str, threading.Lock] = {}
        self._manager_lock = threading.Lock()

    def acquire(self, key: str) -> threading.Lock | None:
        with self._manager_lock:
            lock = self._locks.setdefault(key, threading.Lock())
        if not lock.acquire(blocking=False):
            return None
        return lock
