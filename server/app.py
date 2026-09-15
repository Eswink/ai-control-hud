from __future__ import annotations

import os
import time
from datetime import datetime, timezone

from fastapi import FastAPI

from .hud.fixtures import load_fixture
from .hud.models import HealthResponse, HealthSources, HudState
from .hud.store import SnapshotStore

APP_VERSION = "0.1.0-dev"
_started_monotonic = time.monotonic()


def create_app(fixture: str | None = None) -> FastAPI:
    initial = load_fixture(fixture or os.getenv("HUD_FIXTURE", "healthy.json"))
    store = SnapshotStore(initial)

    app = FastAPI(title="AI Control HUD", version=APP_VERSION)
    app.state.store = store

    @app.get("/api/v1/state", response_model=HudState, response_model_by_alias=True)
    async def get_state() -> HudState:
        snapshot = await store.get()
        now = datetime.now(timezone.utc)
        return snapshot.model_copy(update={"server": snapshot.server.model_copy(update={"time": now, "uptime_seconds": max(0, int(time.monotonic() - _started_monotonic))})})

    @app.get("/api/v1/health", response_model=HealthResponse, response_model_by_alias=True)
    async def get_health() -> HealthResponse:
        snapshot = await store.get()
        return HealthResponse(time=datetime.now(timezone.utc), sources=HealthSources(zcode=snapshot.zcode.health.status, command_code=snapshot.command_code.health.status))

    return app


app = create_app()
