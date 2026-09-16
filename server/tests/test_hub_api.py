from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path

from fastapi.testclient import TestClient

from server.hub.app import create_hub_app
from server.hub.config import HubConfig
from server.hud.fixtures import load_fixture


def make_config(tmp_path: Path) -> HubConfig:
    return HubConfig(
        database_path=tmp_path / "hub.sqlite3",
        primary_agent_id="desktop-main",
        agent_token="test-agent-token",
        stale_after_seconds=45,
    )


def state_payload(now: datetime) -> dict:
    fixture = load_fixture("healthy.json")
    return {
        "agentId": "desktop-main",
        "sentAt": now.isoformat(),
        "state": fixture.model_dump(mode="json", by_alias=True),
    }


def auth_headers() -> dict[str, str]:
    return {"Authorization": "Bearer test-agent-token"}


def test_health_is_reachable_before_first_agent_snapshot(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)

    with TestClient(app) as client:
        response = client.get("/api/v1/health")
        assert response.status_code == 200
        body = response.json()
        assert body["schemaVersion"] == 1
        assert body["sources"] == {"zcode": "error", "commandCode": "error"}
        assert client.get("/api/v1/state").status_code == 503


def test_agent_ingest_requires_bearer_token(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)

    with TestClient(app) as client:
        response = client.post("/api/v1/agent/state", json=state_payload(now))
        assert response.status_code == 401
        assert response.headers["www-authenticate"] == "Bearer"


def test_ingested_schema_v1_state_is_android_compatible(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)

    with TestClient(app) as client:
        accepted = client.post(
            "/api/v1/agent/state",
            json=state_payload(now),
            headers=auth_headers(),
        )
        assert accepted.status_code == 200
        assert accepted.json()["status"] == "accepted"

        response = client.get("/api/v1/state")
        assert response.status_code == 200
        body = response.json()
        assert body["schemaVersion"] == 1
        assert body["overall"]["status"] == "live"
        assert body["server"]["version"] == "0.2.0-hub-dev"
        assert body["zcode"]["health"]["status"] == "ok"
        assert body["commandCode"]["health"]["status"] == "ok"
        assert body["zcode"]["tasks"]


def test_missing_heartbeat_projects_trusted_sources_as_stale(tmp_path: Path) -> None:
    clock = [datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)]
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: clock[0])

    with TestClient(app) as client:
        accepted = client.post(
            "/api/v1/agent/state",
            json=state_payload(clock[0]),
            headers=auth_headers(),
        )
        assert accepted.status_code == 200

        clock[0] += timedelta(seconds=46)
        response = client.get("/api/v1/state")
        assert response.status_code == 200
        body = response.json()
        assert body["overall"]["status"] == "degraded"
        assert body["zcode"]["health"]["status"] == "stale"
        assert body["commandCode"]["health"]["status"] == "stale"
        assert "heartbeat stale" in body["zcode"]["health"]["message"]
        assert body["zcode"]["tasks"]

        health = client.get("/api/v1/health").json()
        assert health["sources"] == {"zcode": "stale", "commandCode": "stale"}


def test_heartbeat_refreshes_agent_freshness_without_replacing_snapshot(tmp_path: Path) -> None:
    clock = [datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)]
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: clock[0])

    with TestClient(app) as client:
        assert client.post(
            "/api/v1/agent/state",
            json=state_payload(clock[0]),
            headers=auth_headers(),
        ).status_code == 200

        clock[0] += timedelta(seconds=50)
        stale = client.get("/api/v1/state").json()
        assert stale["overall"]["status"] == "degraded"

        heartbeat = client.post(
            "/api/v1/agent/heartbeat",
            json={
                "agentId": "desktop-main",
                "sentAt": clock[0].isoformat(),
                "agentVersion": "0.3.0",
            },
            headers=auth_headers(),
        )
        assert heartbeat.status_code == 200

        fresh = client.get("/api/v1/state").json()
        assert fresh["overall"]["status"] == "live"
        assert fresh["zcode"]["health"]["status"] == "ok"
        assert fresh["commandCode"]["health"]["status"] == "ok"


def test_snapshot_survives_hub_restart(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 11, 0, tzinfo=timezone.utc)
    config = make_config(tmp_path)

    first_app = create_hub_app(config, now_provider=lambda: now)
    with TestClient(first_app) as client:
        assert client.post(
            "/api/v1/agent/state",
            json=state_payload(now),
            headers=auth_headers(),
        ).status_code == 200

    second_app = create_hub_app(config, now_provider=lambda: now + timedelta(seconds=5))
    with TestClient(second_app) as client:
        response = client.get("/api/v1/state")
        assert response.status_code == 200
        assert response.json()["overall"]["status"] == "live"
