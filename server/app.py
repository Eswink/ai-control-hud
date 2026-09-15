from __future__ import annotations

import os
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone

from fastapi import FastAPI

from .hud.adapters.base import CommandCodeAdapter, ZCodeAdapter
from .hud.adapters.commandcode_zcode import CommandCodeZCodeProviderAdapter
from .hud.adapters.zcode_goal import ZCodeCompositeAdapter
from .hud.bootstrap import build_bootstrap_state
from .hud.config import RuntimeConfig
from .hud.fixtures import load_fixture
from .hud.models import HealthResponse, HealthSources, HudState
from .hud.runtime import HudRuntime
from .hud.store import SnapshotStore
from .hud.zcode_provider import (
    ZCodeProviderConfigError,
    default_zcode_provider_configs,
    find_commandcode_provider_across_configs,
)

APP_VERSION = "0.1.0-dev"


def create_app(
    fixture: str | None = None,
    *,
    zcode_adapter: ZCodeAdapter | None = None,
    command_code_adapter: CommandCodeAdapter | None = None,
    runtime_config: RuntimeConfig | None = None,
) -> FastAPI:
    fixture_name = fixture if fixture is not None else os.getenv("HUD_FIXTURE")
    initial = load_fixture(fixture_name) if fixture_name else build_bootstrap_state(APP_VERSION)
    store = SnapshotStore(initial)
    runtime = HudRuntime(
        store,
        zcode_adapter=zcode_adapter,
        command_code_adapter=command_code_adapter,
        config=runtime_config or RuntimeConfig.from_env(),
    )
    started_monotonic = time.monotonic()

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        await runtime.start()
        try:
            yield
        finally:
            await runtime.stop()

    app = FastAPI(title="AI Control HUD", version=APP_VERSION, lifespan=lifespan)
    app.state.store = store
    app.state.runtime = runtime

    @app.get("/api/v1/state", response_model=HudState, response_model_by_alias=True)
    async def get_state() -> HudState:
        snapshot = await store.get()
        now = datetime.now(timezone.utc)
        return snapshot.model_copy(
            update={
                "server": snapshot.server.model_copy(
                    update={
                        "time": now,
                        "uptime_seconds": max(
                            0, int(time.monotonic() - started_monotonic)
                        ),
                    }
                )
            }
        )

    @app.get(
        "/api/v1/health",
        response_model=HealthResponse,
        response_model_by_alias=True,
    )
    async def get_health() -> HealthResponse:
        snapshot = await store.get()
        return HealthResponse(
            time=datetime.now(timezone.utc),
            sources=HealthSources(
                zcode=snapshot.zcode.health.status,
                command_code=snapshot.command_code.health.status,
            ),
        )

    return app


def _commandcode_adapter_from_zcode() -> CommandCodeZCodeProviderAdapter | None:
    configs = default_zcode_provider_configs()
    if not any(path.is_file() for path in configs):
        return None
    explicit = os.getenv("HUD_COMMANDCODE_PROVIDER_ID") or None
    try:
        found = find_commandcode_provider_across_configs(
            configs,
            explicit_provider_id=explicit,
        )
    except ZCodeProviderConfigError:
        first_existing = next((path for path in configs if path.is_file()), configs[0])
        return CommandCodeZCodeProviderAdapter(
            first_existing,
            provider_id=explicit,
            allow_nonofficial_provider=bool(explicit),
        )
    if found is None:
        return None
    config_path, provider = found
    return CommandCodeZCodeProviderAdapter(
        config_path,
        provider_id=provider.provider_id,
        allow_nonofficial_provider=bool(explicit),
    )


def create_production_app() -> FastAPI:
    fixture_name = os.getenv("HUD_FIXTURE")
    if fixture_name:
        return create_app(fixture=fixture_name)
    return create_app(
        zcode_adapter=ZCodeCompositeAdapter.from_environment(),
        command_code_adapter=_commandcode_adapter_from_zcode(),
    )


app = create_production_app()
