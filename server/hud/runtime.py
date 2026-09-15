from __future__ import annotations

import asyncio
from datetime import datetime, timezone

from .adapters.base import CommandCodeAdapter, PublicAdapterError, ZCodeAdapter
from .config import RuntimeConfig
from .models import CommandCodeState, SourceHealth, ZCodeState
from .store import SnapshotStore


def _utcnow() -> datetime:
    return datetime.now(timezone.utc)


def _public_error_message(error: Exception) -> str:
    if isinstance(error, PublicAdapterError):
        message = str(error).strip() or "source collection failed"
        return message[:240]
    return f"{type(error).__name__}: source collection failed"[:240]


class HudRuntime:
    """Run source collectors independently and commit normalized state to the store."""

    def __init__(
        self,
        store: SnapshotStore,
        *,
        zcode_adapter: ZCodeAdapter | None = None,
        command_code_adapter: CommandCodeAdapter | None = None,
        config: RuntimeConfig | None = None,
    ):
        self.store = store
        self.zcode_adapter = zcode_adapter
        self.command_code_adapter = command_code_adapter
        self.config = config or RuntimeConfig()
        self._tasks: list[asyncio.Task[None]] = []

    async def start(self) -> None:
        if self._tasks:
            return
        if self.zcode_adapter is not None:
            self._tasks.append(
                asyncio.create_task(self._zcode_loop(), name="hud-zcode-collector")
            )
        if self.command_code_adapter is not None:
            self._tasks.append(
                asyncio.create_task(
                    self._command_code_loop(), name="hud-commandcode-collector"
                )
            )

    async def stop(self) -> None:
        tasks, self._tasks = self._tasks, []
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    async def collect_zcode_once(self) -> None:
        if self.zcode_adapter is None:
            return
        try:
            payload = await self.zcode_adapter.collect()
            observed_at = _utcnow()
            state = ZCodeState(
                health=SourceHealth(
                    status="ok",
                    observed_at=observed_at,
                    last_success_at=observed_at,
                    message=None,
                ),
                summary=payload.summary,
                tasks=payload.tasks,
            )
        except Exception as error:
            await self._record_zcode_failure(_utcnow(), error)
            return
        await self.store.update_zcode(state)

    async def collect_command_code_once(self) -> None:
        if self.command_code_adapter is None:
            return
        try:
            payload = await self.command_code_adapter.collect()
            observed_at = _utcnow()
            state = CommandCodeState(
                health=SourceHealth(
                    status="ok",
                    observed_at=observed_at,
                    last_success_at=observed_at,
                    message=None,
                ),
                usage=payload.usage,
            )
        except Exception as error:
            await self._record_command_code_failure(_utcnow(), error)
            return
        await self.store.update_command_code(state)

    async def _record_zcode_failure(
        self, observed_at: datetime, error: Exception
    ) -> None:
        previous = (await self.store.get()).zcode
        has_last_good = (
            previous.summary is not None
            and previous.tasks is not None
            and previous.health.last_success_at is not None
        )
        if has_last_good:
            state = ZCodeState(
                health=SourceHealth(
                    status="stale",
                    observed_at=observed_at,
                    last_success_at=previous.health.last_success_at,
                    message=_public_error_message(error),
                ),
                summary=previous.summary,
                tasks=previous.tasks,
            )
        else:
            state = ZCodeState(
                health=SourceHealth(
                    status="error",
                    observed_at=observed_at,
                    last_success_at=None,
                    message=_public_error_message(error),
                ),
                summary=None,
                tasks=None,
            )
        await self.store.update_zcode(state)

    async def _record_command_code_failure(
        self, observed_at: datetime, error: Exception
    ) -> None:
        previous = (await self.store.get()).command_code
        has_last_good = (
            previous.usage is not None and previous.health.last_success_at is not None
        )
        if has_last_good:
            state = CommandCodeState(
                health=SourceHealth(
                    status="stale",
                    observed_at=observed_at,
                    last_success_at=previous.health.last_success_at,
                    message=_public_error_message(error),
                ),
                usage=previous.usage,
            )
        else:
            state = CommandCodeState(
                health=SourceHealth(
                    status="error",
                    observed_at=observed_at,
                    last_success_at=None,
                    message=_public_error_message(error),
                ),
                usage=None,
            )
        await self.store.update_command_code(state)

    async def _zcode_loop(self) -> None:
        while True:
            await self.collect_zcode_once()
            await asyncio.sleep(self.config.zcode_interval_seconds)

    async def _command_code_loop(self) -> None:
        while True:
            await self.collect_command_code_once()
            await asyncio.sleep(self.config.command_code_interval_seconds)
