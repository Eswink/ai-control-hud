from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

SourceStatus = Literal["ok", "stale", "error", "disabled"]
TaskStatus = Literal["running", "waiting", "failed", "completed", "unknown"]
OverallStatusValue = Literal["live", "degraded"]


def _to_camel(value: str) -> str:
    head, *tail = value.split("_")
    return head + "".join(part[:1].upper() + part[1:] for part in tail)


class ApiModel(BaseModel):
    model_config = ConfigDict(alias_generator=_to_camel, populate_by_name=True, extra="forbid")


class SourceHealth(ApiModel):
    status: SourceStatus
    observed_at: datetime
    last_success_at: datetime | None = None
    message: str | None = Field(default=None, max_length=240)

    @model_validator(mode="after")
    def validate_health(self) -> "SourceHealth":
        if self.observed_at.tzinfo is None:
            raise ValueError("observedAt must include a timezone")
        if self.last_success_at is not None:
            if self.last_success_at.tzinfo is None:
                raise ValueError("lastSuccessAt must include a timezone")
            if self.last_success_at > self.observed_at:
                raise ValueError("lastSuccessAt cannot be after observedAt")
        if self.status in {"ok", "stale"} and self.last_success_at is None:
            raise ValueError(f"{self.status} health requires lastSuccessAt")
        return self


class TaskChanges(ApiModel):
    additions: int | None = Field(default=None, ge=0)
    deletions: int | None = Field(default=None, ge=0)


class TaskSummary(ApiModel):
    id: str = Field(min_length=1, max_length=256)
    title: str = Field(min_length=1, max_length=500)
    workspace: str | None = Field(default=None, max_length=500)
    status: TaskStatus
    started_at: datetime | None = None
    updated_at: datetime | None = None
    duration_seconds: int | None = Field(default=None, ge=0)
    activity: str | None = Field(default=None, max_length=500)
    changes: TaskChanges | None = None

    @model_validator(mode="after")
    def validate_timestamps(self) -> "TaskSummary":
        for field_name in ("started_at", "updated_at"):
            value = getattr(self, field_name)
            if value is not None and value.tzinfo is None:
                raise ValueError(f"{_to_camel(field_name)} must include a timezone")
        return self


class ZCodeSummary(ApiModel):
    running: int = Field(ge=0)
    waiting: int = Field(ge=0)
    failed: int = Field(ge=0)
    completed: int = Field(ge=0)


class ZCodeState(ApiModel):
    health: SourceHealth
    summary: ZCodeSummary | None = None
    tasks: list[TaskSummary] | None = None

    @model_validator(mode="after")
    def validate_data_availability(self) -> "ZCodeState":
        if self.health.status in {"ok", "stale"}:
            if self.summary is None or self.tasks is None:
                raise ValueError("ok/stale ZCode state requires summary and tasks")
        elif self.health.status in {"error", "disabled"}:
            if self.summary is not None or self.tasks is not None:
                raise ValueError("error/disabled ZCode state must not expose untrusted data")
        return self


class CreditBalance(ApiModel):
    remaining: float | None = Field(default=None, ge=0)
    limit: float | None = Field(default=None, gt=0)
    unit: str | None = Field(default=None, min_length=1, max_length=32)

    @model_validator(mode="after")
    def validate_balance(self) -> "CreditBalance":
        if self.remaining is not None and self.limit is not None and self.remaining > self.limit:
            raise ValueError("credit remaining cannot exceed limit")
        return self


class UsageWindow(ApiModel):
    name: str = Field(min_length=1, max_length=64)
    used_percent: float | None = Field(default=None, ge=0, le=100)
    reset_at: datetime | None = None

    @model_validator(mode="after")
    def validate_reset(self) -> "UsageWindow":
        if self.reset_at is not None and self.reset_at.tzinfo is None:
            raise ValueError("resetAt must include a timezone")
        return self


class UsageSummary(ApiModel):
    plan: str | None = Field(default=None, max_length=128)
    credit: CreditBalance | None = None
    windows: list[UsageWindow] = Field(default_factory=list)

    @model_validator(mode="after")
    def unique_windows(self) -> "UsageSummary":
        names = [item.name for item in self.windows]
        if len(names) != len(set(names)):
            raise ValueError("usage window names must be unique")
        return self


class CommandCodeState(ApiModel):
    health: SourceHealth
    usage: UsageSummary | None = None

    @model_validator(mode="after")
    def validate_data_availability(self) -> "CommandCodeState":
        if self.health.status in {"ok", "stale"} and self.usage is None:
            raise ValueError("ok/stale CommandCode state requires usage")
        if self.health.status in {"error", "disabled"} and self.usage is not None:
            raise ValueError("error/disabled CommandCode state must not expose untrusted data")
        return self


class ServerInfo(ApiModel):
    version: str = Field(min_length=1, max_length=64)
    time: datetime
    uptime_seconds: int = Field(ge=0)

    @model_validator(mode="after")
    def validate_time(self) -> "ServerInfo":
        if self.time.tzinfo is None:
            raise ValueError("server.time must include a timezone")
        return self


class OverallStatus(ApiModel):
    status: OverallStatusValue


class HudState(ApiModel):
    schema_version: Literal[1] = 1
    server: ServerInfo
    overall: OverallStatus
    zcode: ZCodeState
    command_code: CommandCodeState

    @model_validator(mode="after")
    def validate_overall_status(self) -> "HudState":
        expected = "live" if self.zcode.health.status == "ok" and self.command_code.health.status == "ok" else "degraded"
        if self.overall.status != expected:
            raise ValueError(f"overall.status must be {expected!r} for current source health")
        return self


class HealthSources(ApiModel):
    zcode: SourceStatus
    command_code: SourceStatus


class HealthResponse(ApiModel):
    status: Literal["ok"] = "ok"
    schema_version: Literal[1] = 1
    time: datetime
    sources: HealthSources

    @model_validator(mode="after")
    def validate_time(self) -> "HealthResponse":
        if self.time.tzinfo is None:
            raise ValueError("health.time must include a timezone")
        return self
