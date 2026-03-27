from __future__ import annotations

import asyncio


class NamespaceLockManager:
    def __init__(self) -> None:
        self._locks: dict[str, asyncio.Lock] = {}
        self._manager_lock = asyncio.Lock()

    async def get_lock(self, namespace: str) -> asyncio.Lock:
        async with self._manager_lock:
            lock = self._locks.get(namespace)
            if lock is None:
                lock = asyncio.Lock()
                self._locks[namespace] = lock
            return lock

