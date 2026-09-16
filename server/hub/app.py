from __future__ import annotations

import secrets
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Callable

from fastapi import FastAPI, Header, HTTPException, Query, status

from server.hud.models import (
    CommandCodeState,
    HealthResponse,
    HealthSources,
    HudState,
    OverallStatus,
    SourceHealth,
    ZCodeState,
)

from .config import HubConfig
from .models import (
    AgentEventBatch,
    AgentHeartbeat,
    AgentReceipt,
    AgentStateEnvelope,
    EventPage,
    EventReceipt,
)
from .store import HubStore

HUB_VERSION = "0.2.0-hub-dev"
NowProvider = Callable[[], datetime]


def create_hub_app(
    config: HubConfig,
    *,
    store: HubStore | None = None,
    now_provider: NowProvider | None = None,
) -> FastAPI:
    hub_store = store or HubStore(config.database_path)
    owns_store = store is None
    now = now_provider or (lambda: datetime.now(timezone.utc))
    started_monotonic = time.monotonic()

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        try:
            yield
        finally:
            if owns_store:
                hub_store.close()

    app = FastAPI(title="AI Control Hub", version=HUB_VERSION, lifespan=lifespan)
    app.state.hub_store = hub_store
    app.state.hub_config = config

    def require_agent_token(authorization: str | None = Header(default=None)) -> None:
        expected = f"Bearer {config.agent_token}"
        if authorization is None or not secrets.compare_digest(authorization, expected):
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="invalid agent token",
                headers={"WWW-Authenticate": "Bearer"},
            )

    @app.post(
        "/api/v1/agent/state",
        response_model=AgentReceipt,
        response_model_by_alias=True,
        dependencies=[],
    )
    async def ingest_state(
        envelope: AgentStateEnvelope,
        authorization: str | None = Header(default=None),
    ) -> AgentReceipt:
        require_agent_token(authorization)
        received_at = _utc(now())
        hub_store.record_state(
            envelope.agent_id,
            envelope.sent_at,
            envelope.state,
            received_at,
        )
        return AgentReceipt(agent_id=envelope.agent_id, received_at=received_at)

    @app.post(
        "/api/v1/agent/heartbeat",
        response_model=AgentReceipt,
        response_model_by_alias=True,
    )
    async def ingest_heartbeat(
        heartbeat: AgentHeartbeat,
        authorization: str | None = Header(default=None),
    ) -> AgentReceipt:
        require_agent_token(authorization)
        received_at = _utc(now())
        hub_store.record_heartbeat(
            heartbeat.agent_id,
            heartbeat.sent_at,
            received_at,
            heartbeat.agent_version,
        )
        return AgentReceipt(agent_id=heartbeat.agent_id, received_at=received_at)

    @app.post(
        "/api/v1/agent/events",
        response_model=EventReceipt,
        response_model_by_alias=True,
    )
    async def ingest_events(
        batch: AgentEventBatch,
        authorization: str | None = Header(default=None),
    ) -> EventReceipt:
        require_agent_token(authorization)
        received_at = _utc(now())
        accepted, duplicates = hub_store.record_events(
            batch.agent_id,
            batch.sent_at,
            batch.events,
            received_at,
        )
        return EventReceipt(
            agent_id=batch.agent_id,
            received_at=received_at,
            accepted=accepted,
            duplicates=duplicates,
        )

    @app.get("/api/v1/state", response_model=HudState, response_model_by_alias=True)
    async def get_state() -> HudState:
        loaded = hub_store.load_state(config.primary_agent_id)
        if loaded is None:
            raise HTTPException(
                status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
                detail="primary agent has not uploaded a state snapshot",
            )
        snapshot, last_seen_at = loaded
        current = _utc(now())
        return project_state(
            snapshot,
            last_seen_at=last_seen_at,
            now=current,
            stale_after_seconds=config.stale_after_seconds,
            uptime_seconds=max(0, int(time.monotonic() - started_monotonic)),
        )

    @app.get(
        "/api/v1/health",
        response_model=HealthResponse,
        response_model_by_alias=True,
    )
    async def get_health() -> HealthResponse:
        current = _utc(now())
        loaded = hub_store.load_state(config.primary_agent_id)
        if loaded is None:
            return HealthResponse(
                time=current,
                sources=HealthSources(zcode="error", command_code="error"),
            )
        snapshot, last_seen_at = loaded
        projected = project_state(
            snapshot,
            last_seen_at=last_seen_at,
            now=current,
            stale_after_seconds=config.stale_after_seconds,
            uptime_seconds=max(0, int(time.monotonic() - started_monotonic)),
        )
        return HealthResponse(
            time=current,
            sources=HealthSources(
                zcode=projected.zcode.health.status,
                command_code=projected.command_code.health.status,
            ),
        )

    @app.get(
        "/api/v1/events",
        response_model=EventPage,
        response_model_by_alias=True,
    )
    async def get_events(
        after: int = Query(default=0, ge=0),
        limit: int = Query(default=100, ge=1, le=100),
    ) -> EventPage:
        events = hub_store.list_events(config.primary_agent_id, after, limit)
        next_after = events[-1].seq if events else after
        return EventPage(events=events, next_after=next_after)

    return app


def project_state(
    snapshot: HudState,
    *,
    last_seen_at: datetime,
    now: datetime,
    stale_after_seconds: int,
    uptime_seconds: int,
) -> HudState:
    last_seen_at = _utc(last_seen_at)
    now = _utc(now)
    offline_age = max(0.0, (now - last_seen_at).total_seconds())

    zcode = snapshot.zcode
    command_code = snapshot.command_code
    if offline_age > stale_after_seconds:
        message = f"agent heartbeat stale; last seen {int(offline_age)}s ago"
        zcode = _project_zcode_stale(zcode, now, message)
        command_code = _project_command_code_stale(command_code, now, message)

    overall = "live"
    if zcode.health.status != "ok" or command_code.health.status != "ok":
        overall = "degraded"

    return snapshot.model_copy(
        update={
            "server": snapshot.server.model_copy(
                update={
                    "version": HUB_VERSION,
                    "time": now,
                    "uptime_seconds": uptime_seconds,
                }
            ),
            "overall": OverallStatus(status=overall),
            "zcode": zcode,
            "command_code": command_code,
        }
    )


def _project_zcode_stale(
    state: ZCodeState,
    observed_at: datetime,
    message: str,
) -> ZCodeState:
    if state.health.status != "ok":
        return state
    return state.model_copy(
        update={"health": _stale_health(state.health, observed_at, message)}
    )


def _project_command_code_stale(
    state: CommandCodeState,
    observed_at: datetime,
    message: str,
) -> CommandCodeState:
    if state.health.status != "ok":
        return state
    return state.model_copy(
        update={"health": _stale_health(state.health, observed_at, message)}
    )


def _stale_health(
    health: SourceHealth,
    observed_at: datetime,
    message: str,
) -> SourceHealth:
    return health.model_copy(
        update={
            "status": "stale",
            "observed_at": observed_at,
            "message": message,
        }
    )


def _utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        raise ValueError("datetime must include a timezone")
    return value.astimezone(timezone.utc)
