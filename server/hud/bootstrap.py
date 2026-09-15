from __future__ import annotations

from datetime import datetime, timezone

from .models import CommandCodeState, HudState, OverallStatus, ServerInfo, SourceHealth, ZCodeState


def build_bootstrap_state(version: str = "0.1.0-dev") -> HudState:
    """Safe startup state: never make synthetic fixture data look live by default."""
    now = datetime.now(timezone.utc)
    return HudState(
        server=ServerInfo(version=version, time=now, uptime_seconds=0),
        overall=OverallStatus(status="degraded"),
        zcode=ZCodeState(
            health=SourceHealth(
                status="disabled",
                observed_at=now,
                last_success_at=None,
                message="ZCode adapter is not configured",
            ),
            summary=None,
            tasks=None,
        ),
        command_code=CommandCodeState(
            health=SourceHealth(
                status="disabled",
                observed_at=now,
                last_success_at=None,
                message="CommandCode adapter is not configured",
            ),
            usage=None,
        ),
    )
