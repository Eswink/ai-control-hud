from __future__ import annotations

import argparse
import sqlite3
import time
from pathlib import Path


def now_ms() -> int:
    return int(time.time() * 1000)


def init_runtime(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        path.unlink()
    now = now_ms()
    with sqlite3.connect(path) as db:
        db.executescript(
            """
            CREATE TABLE session (
                id TEXT PRIMARY KEY,
                directory TEXT NOT NULL,
                path TEXT,
                title TEXT NOT NULL,
                summary_additions INTEGER,
                summary_deletions INTEGER
            );
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
            );
            CREATE TABLE todo (
                session_id TEXT NOT NULL,
                content TEXT NOT NULL,
                status TEXT NOT NULL,
                priority TEXT NOT NULL,
                position INTEGER NOT NULL,
                time_created INTEGER NOT NULL,
                time_updated INTEGER NOT NULL,
                PRIMARY KEY (session_id, position)
            );
            """
        )
        db.execute(
            "INSERT INTO session VALUES (?, ?, ?, ?, ?, ?)",
            (
                "g4-session",
                r"D:\\synthetic\\g4-workspace",
                r"D:\\synthetic\\g4-workspace",
                "G4 synthetic Goal",
                3,
                1,
            ),
        )
        db.execute(
            "INSERT INTO session_target VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                "g4-session",
                "g4-target",
                "Verify deterministic migration parity",
                "active",
                None,
                1,
                60,
                now - 60_000,
                now,
                "G4 synthetic Goal",
                "g4-input",
                now - 60_000,
                now,
            ),
        )
        db.execute(
            "INSERT INTO todo VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                "g4-session",
                "synthetic running activity",
                "running",
                "normal",
                0,
                now,
                now,
            ),
        )


def init_task_index(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists():
        path.unlink()
    with sqlite3.connect(path) as db:
        db.execute(
            """
            CREATE TABLE tasks (
                workspace_key TEXT NOT NULL,
                workspace_path TEXT NOT NULL,
                workspace_identity TEXT,
                task_id TEXT NOT NULL,
                title TEXT NOT NULL,
                task_status TEXT,
                provider TEXT,
                mode TEXT NOT NULL DEFAULT '',
                model TEXT,
                migration_source TEXT,
                forked_from_task_id TEXT,
                created_at INTEGER NOT NULL DEFAULT 0,
                updated_at INTEGER NOT NULL,
                unread_at INTEGER,
                last_unread_at INTEGER NOT NULL DEFAULT 0,
                pinned INTEGER NOT NULL,
                archived INTEGER NOT NULL,
                deleted INTEGER NOT NULL,
                title_overridden INTEGER NOT NULL DEFAULT 0,
                meta_json TEXT NOT NULL DEFAULT '{}',
                searchable_text TEXT NOT NULL DEFAULT '',
                cron_automation_id TEXT,
                off_peak_task_id TEXT,
                PRIMARY KEY (workspace_key, task_id)
            )
            """
        )


def complete(path: Path) -> None:
    now = now_ms()
    with sqlite3.connect(path) as db:
        db.execute(
            """
            UPDATE session_target
            SET status='completed', time_updated=?, active_run_last_seen_at=?
            WHERE session_id='g4-session'
            """,
            (now, now),
        )
        db.execute(
            """
            UPDATE todo
            SET status='completed', time_updated=?
            WHERE session_id='g4-session'
            """,
            (now,),
        )


def idle(path: Path) -> None:
    old = now_ms() - 3_600_000
    with sqlite3.connect(path) as db:
        db.execute(
            """
            UPDATE session_target
            SET status='completed', time_updated=?, active_run_last_seen_at=NULL
            WHERE session_id='g4-session'
            """,
            (old,),
        )
        db.execute(
            """
            UPDATE todo
            SET status='completed', time_updated=?
            WHERE session_id='g4-session'
            """,
            (old,),
        )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("init", "complete", "idle"))
    parser.add_argument("--runtime", required=True, type=Path)
    parser.add_argument("--task-index", type=Path)
    args = parser.parse_args()

    if args.command == "init":
        if args.task_index is None:
            parser.error("--task-index is required for init")
        init_runtime(args.runtime)
        init_task_index(args.task_index)
    elif args.command == "complete":
        complete(args.runtime)
    else:
        idle(args.runtime)


if __name__ == "__main__":
    main()
