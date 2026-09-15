from __future__ import annotations

from dataclasses import dataclass

from .base import CommandCodePayload, ZCodePayload


@dataclass
class StaticZCodeAdapter:
    payload: ZCodePayload
    error: Exception | None = None

    async def collect(self) -> ZCodePayload:
        if self.error is not None:
            raise self.error
        return self.payload


@dataclass
class StaticCommandCodeAdapter:
    payload: CommandCodePayload
    error: Exception | None = None

    async def collect(self) -> CommandCodePayload:
        if self.error is not None:
            raise self.error
        return self.payload
