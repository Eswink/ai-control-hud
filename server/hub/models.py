from __future__ import annotations

from datetime import datetime

from pydantic import Field, model_validator

from server.hud.models import ApiModel, HudState


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
