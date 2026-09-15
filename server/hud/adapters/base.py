from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from ..models import TaskSummary, UsageSummary, ZCodeSummary


class PublicAdapterError(RuntimeError):
    """Adapter error whose message is intentionally safe for UI/diagnostics."""


@dataclass(frozen=True)
class ZCodePayload:
    summary: ZCodeSummary
    tasks: list[TaskSummary]


@dataclass(frozen=True)
class CommandCodePayload:
    usage: UsageSummary


class ZCodeAdapter(Protocol):
    async def collect(self) -> ZCodePayload:
        ...


class CommandCodeAdapter(Protocol):
    async def collect(self) -> CommandCodePayload:
        ...
