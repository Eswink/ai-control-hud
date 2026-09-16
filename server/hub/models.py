from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import Field, model_validator

from server.hud.models import ApiModel, HudState

EventType = Literal["task.completed", "task.failed"]
TerminalTaskStatus = Literal["completed", "failed"]


class AgentStateEnvelope(ApiModel):
    agent_id: str = Field(min_length=1, max_length=128)
    sent_at: datetime
    state: HudState

    @model_validator(mode="after")
    def validate_sent_at(self) -> "AgentStateEnvelope":
        if self.sent_at.tzinfo is None:
            raise ValueError("sentAt must include a timezone")
        return self


class AgentHeartbeat(ApiModel):
    agent_id: str = Field(min_length=1, max_length=128)
    sent_at: datetime
    agent_version: str | None = Field(default=None, max_length=64)

    @model_validator(mode="after")
    def validate_sent_at(self) -> "AgentHeartbeat":
        if self.sent_at.tzinfo is None:
            raise ValueError("sentAt must include a timezone")
        return self


class AgentReceipt(ApiModel):
    status: str = "accepted"
    agent_id: str
    received_at: datetime


class EventTask(ApiModel):
    id: str = Field(min_length=1, max_length=256)
    title: str = Field(min_length=1, max_length=500)
    workspace: str | None = Field(default=None, max_length=500)
    status: TerminalTaskStatus


class AgentEvent(ApiModel):
    event_id: str = Field(min_length=8, max_length=128)
    type: EventType
    occurred_at: datetime
    task: EventTask

    @model_validator(mode="after")
    def validate_event(self) -> "AgentEvent":
        if self.occurred_at.tzinfo is None:
            raise ValueError("occurredAt must include a timezone")
        expected_status = self.type.removeprefix("task.")
        if self.task.status != expected_status:
            raise ValueError("event type and task status must agree")
        return self


class AgentEventBatch(ApiModel):
    agent_id: str = Field(min_length=1, max_length=128)
    sent_at: datetime
    events: list[AgentEvent] = Field(min_length=1, max_length=100)

    @model_validator(mode="after")
    def validate_sent_at(self) -> "AgentEventBatch":
        if self.sent_at.tzinfo is None:
            raise ValueError("sentAt must include a timezone")
        return self


class EventReceipt(ApiModel):
    status: str = "accepted"
    agent_id: str
    received_at: datetime
    accepted: int = Field(ge=0)
    duplicates: int = Field(ge=0)


class HubEvent(AgentEvent):
    seq: int = Field(ge=1)
    agent_id: str = Field(min_length=1, max_length=128)
    received_at: datetime


class EventPage(ApiModel):
    schema_version: Literal[1] = 1
    events: list[HubEvent]
    next_after: int = Field(ge=0)
    latest_seq: int = Field(ge=0)
