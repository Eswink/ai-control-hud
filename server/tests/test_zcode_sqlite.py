from __future__ import annotations

import asyncio
import sqlite3
from pathlib import Path

import pytest

from server.hud.adapters.base import PublicAdapterError
from server.hud.adapters.zcode_sqlite import ZCodeSQLiteAdapter


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


def test_collects_visible_tasks_and_maps_conservative_statuses(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)

    payload = asyncio.run(ZCodeSQLiteAdapter(db).collect())

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
    assert payload.summary.running == 1
    assert payload.summary.waiting == 1
    assert payload.summary.completed == 1
    assert payload.summary.failed == 0


def test_respects_task_limit(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    _create_task_index(db)

    payload = asyncio.run(ZCodeSQLiteAdapter(db, task_limit=2).collect())

    assert len(payload.tasks) == 2


def test_missing_or_changed_schema_fails_visibly(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    connection = sqlite3.connect(db)
    try:
        connection.execute("CREATE TABLE tasks (task_id TEXT PRIMARY KEY)")
        connection.commit()
    finally:
        connection.close()

    with pytest.raises(PublicAdapterError, match="Unsupported ZCode task index schema"):
        asyncio.run(ZCodeSQLiteAdapter(db).collect())


def test_missing_database_has_safe_public_error(tmp_path: Path) -> None:
    with pytest.raises(PublicAdapterError, match="ZCode task index not found"):
        asyncio.run(ZCodeSQLiteAdapter(tmp_path / "missing.sqlite").collect())
