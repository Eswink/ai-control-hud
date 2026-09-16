from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class HubConfig:
    database_path: Path
    primary_agent_id: str
    agent_token: str
    stale_after_seconds: int = 45

    @classmethod
    def from_env(cls) -> "HubConfig":
        database_path = Path(os.getenv("HUD_HUB_DB", ".local/hub.sqlite3"))
        primary_agent_id = os.getenv("HUD_HUB_AGENT_ID", "desktop-main").strip()
        agent_token = os.getenv("HUD_HUB_AGENT_TOKEN", "").strip()
        stale_raw = os.getenv("HUD_HUB_STALE_AFTER_SECONDS", "45").strip()

        if not primary_agent_id:
            raise RuntimeError("HUD_HUB_AGENT_ID must not be empty")
        if not agent_token:
            raise RuntimeError("HUD_HUB_AGENT_TOKEN is required")
        try:
            stale_after_seconds = int(stale_raw)
        except ValueError as exc:
            raise RuntimeError("HUD_HUB_STALE_AFTER_SECONDS must be an integer") from exc
        if stale_after_seconds < 5:
            raise RuntimeError("HUD_HUB_STALE_AFTER_SECONDS must be at least 5")

        return cls(
            database_path=database_path,
            primary_agent_id=primary_agent_id,
            agent_token=agent_token,
            stale_after_seconds=stale_after_seconds,
        )
