from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path

from fastapi.testclient import TestClient

from server.hub.app import create_hub_app
from server.hub.config import HubConfig


def make_config(tmp_path: Path) -> HubConfig:
    return HubConfig(
        database_path=tmp_path / "hub.sqlite3",
        primary_agent_id="desktop-main",
        agent_token="test-agent-token",
        stale_after_seconds=45,
    )


def auth_headers() -> dict[str, str]:
    return {"Authorization": "Bearer test-agent-token"}


def task_event(
    event_id: str,
    event_type: str,
    occurred_at: datetime,
    *,
    task_id: str = "task-1",
    title: str = "Refactor agent pipeline",
) -> dict:
    status = event_type.removeprefix("task.")
    return {
        "eventId": event_id,
        "type": event_type,
        "occurredAt": occurred_at.isoformat(),
        "task": {
            "id": task_id,
            "title": title,
            "workspace": "backend",
            "status": status,
        },
    }


def event_batch(agent_id: str, sent_at: datetime, events: list[dict]) -> dict:
    return {
        "agentId": agent_id,
        "sentAt": sent_at.isoformat(),
        "events": events,
    }


def test_event_ingest_requires_agent_token(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)

    with TestClient(app) as client:
        response = client.post(
            "/api/v1/agent/events",
            json=event_batch(
                "desktop-main",
                now,
                [task_event("event-0001", "task.completed", now)],
            ),
        )
        assert response.status_code == 401


def test_event_ingest_is_idempotent_and_cursor_ordered(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)
    first = task_event("event-0001", "task.completed", now, task_id="task-1")
    second = task_event(
        "event-0002",
        "task.failed",
        now + timedelta(seconds=1),
        task_id="task-2",
        title="Deploy API",
    )

    with TestClient(app) as client:
        accepted = client.post(
            "/api/v1/agent/events",
            json=event_batch("desktop-main", now, [first, second]),
            headers=auth_headers(),
        )
        assert accepted.status_code == 200
        assert accepted.json()["accepted"] == 2
        assert accepted.json()["duplicates"] == 0

        duplicate = client.post(
            "/api/v1/agent/events",
            json=event_batch("desktop-main", now, [first]),
            headers=auth_headers(),
        )
        assert duplicate.status_code == 200
        assert duplicate.json()["accepted"] == 0
        assert duplicate.json()["duplicates"] == 1

        page = client.get("/api/v1/events?after=0&limit=100")
        assert page.status_code == 200
        body = page.json()
        assert body["schemaVersion"] == 1
        assert [event["eventId"] for event in body["events"]] == [
            "event-0001",
            "event-0002",
        ]
        assert [event["seq"] for event in body["events"]] == [1, 2]
        assert body["events"][0]["task"]["status"] == "completed"
        assert body["events"][1]["task"]["status"] == "failed"
        assert body["nextAfter"] == 2

        tail = client.get("/api/v1/events?after=1&limit=100").json()
        assert tail["schemaVersion"] == 1
        assert [event["eventId"] for event in tail["events"]] == ["event-0002"]
        assert tail["nextAfter"] == 2

        empty = client.get("/api/v1/events?after=2&limit=100").json()
        assert empty == {"schemaVersion": 1, "events": [], "nextAfter": 2}


def test_event_type_and_terminal_status_must_agree(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)
    invalid = task_event("event-0001", "task.completed", now)
    invalid["task"]["status"] = "failed"

    with TestClient(app) as client:
        response = client.post(
            "/api/v1/agent/events",
            json=event_batch("desktop-main", now, [invalid]),
            headers=auth_headers(),
        )
        assert response.status_code == 422


def test_android_event_feed_only_exposes_primary_agent(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)
    app = create_hub_app(make_config(tmp_path), now_provider=lambda: now)

    with TestClient(app) as client:
        assert client.post(
            "/api/v1/agent/events",
            json=event_batch(
                "other-machine",
                now,
                [task_event("other-0001", "task.completed", now)],
            ),
            headers=auth_headers(),
        ).status_code == 200
        assert client.post(
            "/api/v1/agent/events",
            json=event_batch(
                "desktop-main",
                now,
                [task_event("primary-01", "task.completed", now)],
            ),
            headers=auth_headers(),
        ).status_code == 200

        body = client.get("/api/v1/events").json()
        assert body["schemaVersion"] == 1
        assert [event["eventId"] for event in body["events"]] == ["primary-01"]
        assert body["events"][0]["agentId"] == "desktop-main"


def test_events_survive_hub_restart(tmp_path: Path) -> None:
    now = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)
    config = make_config(tmp_path)

    first_app = create_hub_app(config, now_provider=lambda: now)
    with TestClient(first_app) as client:
        response = client.post(
            "/api/v1/agent/events",
            json=event_batch(
                "desktop-main",
                now,
                [task_event("event-0001", "task.completed", now)],
            ),
            headers=auth_headers(),
        )
        assert response.status_code == 200

    second_app = create_hub_app(
        config,
        now_provider=lambda: now + timedelta(seconds=5),
    )
    with TestClient(second_app) as client:
        body = client.get("/api/v1/events?after=0").json()
        assert body["schemaVersion"] == 1
        assert len(body["events"]) == 1
        assert body["events"][0]["eventId"] == "event-0001"
        assert body["nextAfter"] == 1
