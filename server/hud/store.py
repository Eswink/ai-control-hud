from __future__ import annotations

import asyncio

from .models import CommandCodeState, HudState, OverallStatus, ZCodeState


def _overall(zcode: ZCodeState, command_code: CommandCodeState) -> OverallStatus:
    return OverallStatus(
        status="live"
        if zcode.health.status == "ok" and command_code.health.status == "ok"
        else "degraded"
    )


class SnapshotStore:
    """In-memory latest-state store. Vendor I/O must happen outside this class."""

    def __init__(self, initial: HudState):
        self._snapshot = initial.model_copy(deep=True)
        self._lock = asyncio.Lock()

    async def get(self) -> HudState:
        async with self._lock:
            return self._snapshot.model_copy(deep=True)

    async def replace(self, snapshot: HudState) -> None:
        async with self._lock:
            self._snapshot = snapshot.model_copy(deep=True)

    async def update_zcode(self, zcode: ZCodeState) -> None:
        async with self._lock:
            current = self._snapshot
            self._snapshot = HudState(
                schema_version=current.schema_version,
                server=current.server,
                overall=_overall(zcode, current.command_code),
                zcode=zcode,
                command_code=current.command_code,
            )

    async def update_command_code(self, command_code: CommandCodeState) -> None:
        async with self._lock:
            current = self._snapshot
            self._snapshot = HudState(
                schema_version=current.schema_version,
                server=current.server,
                overall=_overall(current.zcode, command_code),
                zcode=current.zcode,
                command_code=command_code,
            )
