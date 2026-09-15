from __future__ import annotations

import asyncio
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

from server.hud.adapters.zcode_goal import (
    ZCodeCompositeAdapter,
    ZCodeGoalAdapter,
)
from server.hud.adapters.zcode_sqlite import ZCodeSQLiteAdapter


def _ms_now() -> int:
    return int(datetime.now(timezone.utc).timestamp() * 1000)


def _create_runtime_db(path: Path, *, heartbeat_ms: int | None = None) -> None:
    now_ms = _ms_now()
    heartbeat_ms = heartbeat_ms if heartbeat_ms is not None else now_ms
    connection = sqlite3.connect(path)
    try:
        connection.execute(
            """
            CREATE TABLE session (
                id TEXT PRIMARY KEY,
                directory TEXT NOT NULL,
                path TEXT,
                title TEXT NOT NULL,
                summary_additions INTEGER,
                summary_deletions INTEGER
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE session_target (
                session_id TEXT PRIMARY KEY,
                target_id TEXT NOT NULL,
                objective TEXT NOT NULL,
                status TEXT NOT NULL,
                token_budget INTEGER,
                tokens_used INTEGER NOT NULL,
                time_used_seconds INTEGER NOT NULL,
                time_created INTEGER NOT NULL,
                time_updated INTEGER NOT NULL,
                summary_title TEXT,
                active_input_id TEXT,
                active_run_started_at INTEGER,
                active_run_last_seen_at INTEGER
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE todo (
                session_id TEXT NOT NULL,
                content TEXT NOT NULL,
                status TEXT NOT NULL,
                priority TEXT NOT NULL,
                position INTEGER NOT NULL,
                time_created INTEGER NOT NULL,
                time_updated INTEGER NOT NULL,
                PRIMARY KEY (session_id, position)
            )
            """
        )
        connection.execute(
            "INSERT INTO session VALUES (?, ?, ?, ?, ?, ?)",
            (
                "session-live",
                r"D:\\d\\research-system",
                r"D:\\d\\research-system",
                "Goal 模式迭代与 collector-quality 持续失败取证",
                42,
                7,
            ),
        )
        connection.execute(
            """
            INSERT INTO session_target VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                "session-live",
                "target-1",
                "持续迭代 goal 文件完善系统",
                "active",
                None,
                123,
                33780,
                now_ms - 40_000,
                now_ms,
                "Goal summary",
                "input-1",
                now_ms - 30_000,
                heartbeat_ms,
            ),
        )
        todos = [
            ("session-live", "completed A", "completed", "normal", 0, now_ms, now_ms),
            ("session-live", "completed B", "completed", "normal", 1, now_ms, now_ms),
            ("session-live", "GOAL-002 cycle 5: local gates", "running", "normal", 2, now_ms, now_ms),
            ("session-live", "write PLAN-059", "pending", "normal", 3, now_ms, now_ms),
        ]
        connection.executemany("INSERT INTO todo VALUES (?, ?, ?, ?, ?, ?, ?)", todos)
        connection.commit()
    finally:
        connection.close()


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
        connection.execute(
            "INSERT INTO tasks VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                "old-ws",
                r"D:\\d\\research-system",
                "old-task",
                "old failed task",
                "failed",
                1787909883153,
                0,
                0,
                0,
            ),
        )
        connection.commit()
    finally:
        connection.close()


def test_live_goal_exposes_current_goal_and_running_todo(tmp_path: Path) -> None:
    runtime_db = tmp_path / "db.sqlite"
    _create_runtime_db(runtime_db)

    payload = asyncio.run(ZCodeGoalAdapter(runtime_db).collect_optional())

    assert payload is not None
    assert payload.summary.running == 1
    assert payload.summary.failed == 0
    assert len(payload.tasks) == 1
    task = payload.tasks[0]
    assert task.status == "running"
    assert task.title == "Goal 模式迭代与 collector-quality 持续失败取证"
    assert task.workspace == "research-system"
    assert task.activity == "GOAL-002 cycle 5: local gates"
    assert task.duration_seconds == 33780
    assert task.changes is not None
    assert task.changes.additions == 42
    assert task.changes.deletions == 7
    assert task.id.startswith("zcode-goal-")
    assert "session-live" not in task.id


def test_composite_prefers_live_goal_over_old_failed_index(tmp_path: Path) -> None:
    runtime_db = tmp_path / "db.sqlite"
    task_db = tmp_path / "tasks-index.sqlite"
    _create_runtime_db(runtime_db)
    _create_task_index(task_db)

    adapter = ZCodeCompositeAdapter(
        ZCodeGoalAdapter(runtime_db),
        ZCodeSQLiteAdapter(task_db),
    )
    payload = asyncio.run(adapter.collect())

    assert payload.summary.running == 1
    assert payload.summary.failed == 0
    assert [task.title for task in payload.tasks] == [
        "Goal 模式迭代与 collector-quality 持续失败取证"
    ]


def test_stale_terminal_goal_falls_back_to_task_index(tmp_path: Path) -> None:
    runtime_db = tmp_path / "db.sqlite"
    task_db = tmp_path / "tasks-index.sqlite"
    old_heartbeat = _ms_now() - (3 * 60 * 60 * 1000)
    _create_runtime_db(runtime_db, heartbeat_ms=old_heartbeat)
    connection = sqlite3.connect(runtime_db)
    try:
        connection.execute("UPDATE session_target SET status='failed', time_updated=?", (old_heartbeat,))
        connection.execute("UPDATE todo SET status='completed'")
        connection.commit()
    finally:
        connection.close()
    _create_task_index(task_db)

    adapter = ZCodeCompositeAdapter(
        ZCodeGoalAdapter(runtime_db, heartbeat_seconds=60, recent_terminal_seconds=60),
        ZCodeSQLiteAdapter(task_db),
    )
    payload = asyncio.run(adapter.collect())

    assert payload.summary.failed == 1
    assert payload.tasks[0].title == "old failed task"


def test_stale_running_goal_is_not_live_even_with_running_todo(tmp_path: Path) -> None:
    runtime_db = tmp_path / "db.sqlite"
    _create_runtime_db(runtime_db)
    stale = _ms_now() - (7 * 24 * 60 * 60 * 1000)

    connection = sqlite3.connect(runtime_db)
    try:
        connection.execute(
            "INSERT INTO session VALUES (?, ?, ?, ?, ?, ?)",
            (
                "session-stale",
                r"D:\\d\\research-system",
                r"D:\\d\\research-system",
                "old stale goal",
                None,
                None,
            ),
        )
        connection.execute(
            """
            INSERT INTO session_target VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                "session-stale",
                "target-old",
                "old objective",
                "active",
                None,
                1,
                999,
                stale,
                stale,
                "old stale goal",
                "old-input",
                stale,
                stale,
            ),
        )
        connection.execute(
            "INSERT INTO todo VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                "session-stale",
                "old running todo",
                "running",
                "normal",
                0,
                stale,
                stale,
            ),
        )
        connection.commit()
    finally:
        connection.close()

    payload = asyncio.run(ZCodeGoalAdapter(runtime_db, heartbeat_seconds=120).collect_optional())

    assert payload is not None
    assert payload.summary.running == 1
    assert [task.title for task in payload.tasks] == [
        "Goal 模式迭代与 collector-quality 持续失败取证"
    ]
