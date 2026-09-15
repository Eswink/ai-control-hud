from __future__ import annotations

import json
import sqlite3
from pathlib import Path

from tools.discovery import (
    COMMANDCODE_CLI_NAMES,
    environment_presence,
    json_metadata,
    sqlite_schema,
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
