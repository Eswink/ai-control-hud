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
    hub_id: str = "central-hub"
    discovery_enabled: bool = False
    discovery_port: int = 8788
    http_scheme: str = "http"
    http_port: int = 8787

    @classmethod
    def from_env(cls) -> "HubConfig":
        database_path = Path(os.getenv("HUD_HUB_DB", ".local/hub.sqlite3"))
        primary_agent_id = os.getenv("HUD_HUB_AGENT_ID", "desktop-main").strip()
        agent_token = os.getenv("HUD_HUB_AGENT_TOKEN", "").strip()
        stale_raw = os.getenv("HUD_HUB_STALE_AFTER_SECONDS", "45").strip()
        hub_id = os.getenv("HUD_HUB_ID", "central-hub").strip()
        discovery_raw = os.getenv("HUD_HUB_DISCOVERY_ENABLED", "0").strip()
        discovery_port_raw = os.getenv("HUD_HUB_DISCOVERY_PORT", "8788").strip()
        http_scheme = os.getenv("HUD_HUB_HTTP_SCHEME", "http").strip().lower()
        http_port_raw = os.getenv("HUD_HUB_HTTP_PORT", "8787").strip()

        if not primary_agent_id:
            raise RuntimeError("HUD_HUB_AGENT_ID must not be empty")
        if not agent_token:
            raise RuntimeError("HUD_HUB_AGENT_TOKEN is required")
        if not hub_id:
            raise RuntimeError("HUD_HUB_ID must not be empty")
        if http_scheme not in {"http", "https"}:
            raise RuntimeError("HUD_HUB_HTTP_SCHEME must be http or https")

        try:
            stale_after_seconds = int(stale_raw)
        except ValueError as exc:
            raise RuntimeError("HUD_HUB_STALE_AFTER_SECONDS must be an integer") from exc
        if stale_after_seconds < 5:
            raise RuntimeError("HUD_HUB_STALE_AFTER_SECONDS must be at least 5")

        discovery_enabled = _parse_bool(discovery_raw, "HUD_HUB_DISCOVERY_ENABLED")
        discovery_port = _parse_port(discovery_port_raw, "HUD_HUB_DISCOVERY_PORT")
        http_port = _parse_port(http_port_raw, "HUD_HUB_HTTP_PORT")

        return cls(
            database_path=database_path,
            primary_agent_id=primary_agent_id,
            agent_token=agent_token,
            stale_after_seconds=stale_after_seconds,
            hub_id=hub_id,
            discovery_enabled=discovery_enabled,
            discovery_port=discovery_port,
            http_scheme=http_scheme,
            http_port=http_port,
        )


def _parse_bool(raw: str, name: str) -> bool:
    value = raw.lower()
    if value in {"1", "true", "yes", "on"}:
        return True
    if value in {"0", "false", "no", "off"}:
        return False
    raise RuntimeError(f"{name} must be a boolean")


def _parse_port(raw: str, name: str) -> int:
    try:
        port = int(raw)
    except ValueError as exc:
        raise RuntimeError(f"{name} must be an integer") from exc
    if port < 1 or port > 65535:
        raise RuntimeError(f"{name} must be within 1..65535")
    return port
