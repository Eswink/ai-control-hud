from __future__ import annotations

import json
import sqlite3
from pathlib import Path

from tools.discovery import (
    COMMANDCODE_CLI_NAMES,
    environment_presence,
    json_metadata,
    sqlite_schema,
    zcode_task_value_summary,
)


def test_sqlite_probe_reads_schema_not_rows(tmp_path: Path) -> None:
    db = tmp_path / "zcode.sqlite"
    conn = sqlite3.connect(db)
    conn.execute("CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT NOT NULL, status TEXT)")
    conn.execute("INSERT INTO tasks VALUES (?, ?, ?)", ("secret-id", "VERY_PRIVATE_TASK", "running"))
    conn.commit()
    conn.close()
    report = sqlite_schema(db)
    serialized = json.dumps(report)
    assert report["readOnly"] is True
    assert report["objects"][0]["name"] == "tasks"
    assert [column["name"] for column in report["objects"][0]["columns"]] == ["id", "title", "status"]
    assert "VERY_PRIVATE_TASK" not in serialized
    assert "secret-id" not in serialized
    assert "running" not in serialized


def test_json_probe_never_includes_values(tmp_path: Path) -> None:
    auth = tmp_path / "auth.json"
    auth.write_text(
        json.dumps(
            {
                "access_token": "TOP_SECRET_TOKEN",
                "expires": 123,
                "nested": {"email": "private@example.com"},
            }
        ),
        encoding="utf-8",
    )
    report = json_metadata(auth)
    serialized = json.dumps(report)
    assert "access_token" in serialized
    assert "TOP_SECRET_TOKEN" not in serialized
    assert "private@example.com" not in serialized
    assert report["shape"]["keys"]["access_token"] == {"type": "string", "length": 16}


def test_windows_commandcode_alias_is_discovered() -> None:
    assert "cmdc" in COMMANDCODE_CLI_NAMES
    assert "command-code" in COMMANDCODE_CLI_NAMES


def test_environment_probe_reports_presence_not_value(monkeypatch) -> None:
    monkeypatch.setenv("COMMAND_CODE_API_KEY", "TOP_SECRET_COMMAND_CODE_KEY")
    report = environment_presence(("COMMAND_CODE_API_KEY",))
    serialized = json.dumps(report)
    assert report == {"COMMAND_CODE_API_KEY": {"present": True}}
    assert "TOP_SECRET_COMMAND_CODE_KEY" not in serialized


def test_zcode_value_summary_reads_only_aggregate_metadata(tmp_path: Path) -> None:
    db = tmp_path / "tasks-index.sqlite"
    conn = sqlite3.connect(db)
    try:
        conn.execute(
            """
            CREATE TABLE tasks (
                workspace_key TEXT NOT NULL,
                workspace_path TEXT NOT NULL,
                task_id TEXT NOT NULL,
                title TEXT NOT NULL,
                task_status TEXT,
                created_at INTEGER NOT NULL,
                updated_at INTEGER NOT NULL,
                archived INTEGER NOT NULL,
                deleted INTEGER NOT NULL,
                PRIMARY KEY (workspace_key, task_id)
            )
            """
        )
        conn.executemany(
            """
            INSERT INTO tasks (
                workspace_key, workspace_path, task_id, title, task_status,
                created_at, updated_at, archived, deleted
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            [
                ("SECRET_WORKSPACE_A", r"D:\\private\\alpha", "SECRET_TASK_1", "PRIVATE TITLE ONE", "running", 1789480000000, 1789480000123, 0, 0),
                ("SECRET_WORKSPACE_B", r"D:\\private\\beta", "SECRET_TASK_2", "PRIVATE TITLE TWO", "queued", 1789480001000, 1789480001456, 0, 0),
                ("SECRET_WORKSPACE_C", r"D:\\private\\gamma", "SECRET_TASK_3", "PRIVATE TITLE THREE", "running", 1789480002000, 1789480002789, 0, 0),
                ("SECRET_WORKSPACE_D", r"D:\\private\\hidden", "SECRET_TASK_4", "PRIVATE ARCHIVED", "failed", 1789480003000, 1789480003000, 1, 0),
            ],
        )
        conn.commit()
    finally:
        conn.close()

    report = zcode_task_value_summary(db)
    serialized = json.dumps(report)

    assert report["readOnly"] is True
    assert report["contentFieldsRead"] is False
    assert report["activeTaskCount"] == 3
    assert report["statusCounts"] == [
        {"status": "running", "count": 2},
        {"status": "queued", "count": 1},
    ]
    assert report["timestamps"]["createdAt"]["inferredUnit"] == "milliseconds"
    assert report["timestamps"]["updatedAt"]["maxDigits"] == 13

    for secret in (
        "SECRET_WORKSPACE_A",
        "SECRET_TASK_1",
        "PRIVATE TITLE ONE",
        r"D:\\private\\alpha",
        "1789480000123",
    ):
        assert secret not in serialized
