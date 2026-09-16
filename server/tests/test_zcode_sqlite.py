from __future__ import annotations

import asyncio
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

import pytest

from server.hud.adapters.base import PublicAdapterError
from server.hud.adapters.zcode_sqlite import ZCodeSQLiteAdapter

_TEST_NOW = datetime(2026, 9, 16, 12, 0, tzinfo=timezone.utc)


def _create_task_index(path: Path) -> None:
    connection = sqlite3.connect(path)
    try:
        connection.execute(
            """
            CREATE TABLE tasks (
                workspace_key TEXT NOT NULL,
                workspace_path TEXT NOT NULL,
                task_id TEXT NOT NULL,
                title TEXT NOT NULL,
                task_status TEXT,
                updated_at INTEGER NOT NULL,
                pinned INTEGER NOT NULL,
                archived INTEGER NOT NULL,
                deleted INTEGER NOT NULL,
                PRIMARY KEY (workspace_key, task_id)
            )
            """
        )
        connection.executemany(
            """
            INSERT INTO tasks (
                workspace_key, workspace_path, task_id, title, task_status,
                updated_at, pinned, archived, deleted
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                ("wk-a", r"D:\\github\\alpha", "task-1", "Running task", "running", 1789480000000, 1, 0, 0),
                ("wk-b", r"D:\\github\\beta", "task-2", "Queued task", "queued", 1789479000000, 0, 0, 0),
                ("wk-c", r"D:\\github\\gamma", "task-3", "Done task", "completed", 1789478000000, 0, 0, 0),
                ("wk-d", r"D:\\github\\delta", "task-4", "Odd state", "brand_new_state", 1789477000000, 0, 0, 0),
                ("wk-e", r"D:\\github\\hidden", "task-5", "Archived", "running", 1789476000000, 0, 1, 0),
            ],
        )
        connection.commit()
    finally:
        connection.close()


def _adapter(db: Path, **kwargs: object) -> ZCodeSQLiteAdapter:
    return ZCodeSQLiteAdapter(db, now_provider=lambda: _TEST_NOW, **kwargs)


def test_collects_visible_tasks_and_maps_conservative_statuses(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)

    payload = asyncio.run(_adapter(db).collect())

    assert [task.title for task in payload.tasks] == [
        "Running task",
        "Queued task",
        "Done task",
        "Odd state",
    ]
    assert [task.status for task in payload.tasks] == [
        "running",
        "waiting",
        "completed",
        "unknown",
    ]
    assert payload.tasks[0].workspace == "alpha"
    assert payload.tasks[0].updated_at is not None
    assert payload.tasks[0].updated_at.tzinfo is not None
    assert payload.tasks[0].id.startswith("zcode-")
    assert "wk-a" not in payload.tasks[0].id
    assert "task-1" not in payload.tasks[0].id
    assert payload.summary.running == 1
    assert payload.summary.waiting == 1
    assert payload.summary.completed == 1
    assert payload.summary.failed == 0


def test_task_limit_does_not_truncate_summary_counts(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)

    payload = asyncio.run(_adapter(db, task_limit=2).collect())

    assert len(payload.tasks) == 2
    assert payload.summary.running == 1
    assert payload.summary.waiting == 1
    assert payload.summary.completed == 1


def test_stale_task_index_rows_are_hidden_from_realtime_hud(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)
    connection = sqlite3.connect(db)
    try:
        connection.execute("UPDATE tasks SET updated_at = ?", (1_700_000_000_000,))
        connection.commit()
    finally:
        connection.close()

    payload = asyncio.run(
        _adapter(db, task_max_age_seconds=3_600).collect()
    )

    assert payload.tasks == []
    assert payload.summary.running == 0
    assert payload.summary.waiting == 0
    assert payload.summary.failed == 0
    assert payload.summary.completed == 0


def test_missing_or_changed_schema_fails_visibly(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    connection = sqlite3.connect(db)
    try:
        connection.execute("CREATE TABLE tasks (task_id TEXT PRIMARY KEY)")
        connection.commit()
    finally:
        connection.close()

    with pytest.raises(PublicAdapterError, match="Unsupported ZCode task index schema"):
        asyncio.run(_adapter(db).collect())


def test_missing_database_has_safe_public_error(tmp_path: Path) -> None:
    with pytest.raises(PublicAdapterError, match="ZCode task index not found"):
        asyncio.run(_adapter(tmp_path / "missing.sqlite").collect())


def test_now_provider_must_be_timezone_aware(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)
    adapter = ZCodeSQLiteAdapter(
        db,
        now_provider=lambda: datetime(2026, 9, 16, 12, 0),
    )
    with pytest.raises(ValueError, match="timezone-aware"):
        asyncio.run(adapter.collect())
