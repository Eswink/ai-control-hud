from __future__ import annotations

import asyncio

from .models import HudState


class SnapshotStore:
    """In-memory latest-state store. Vendor I/O must happen outside this class."""

    def __init__(self, initial: HudState):
        self._snapshot = initial
        self._lock = asyncio.Lock()

    async def get(self) -> HudState:
        async with self._lock:
            return self._snapshot.model_copy(deep=True)

    async def replace(self, snapshot: HudState) -> None:
        async with self._lock:
            self._snapshot = snapshot.model_copy(deep=True)
