from __future__ import annotations

import sqlite3
from pathlib import Path

import pytest

from tools.hub_backup import backup_database


def test_backup_captures_live_wal_database(tmp_path: Path) -> None:
    source = tmp_path / "hub.sqlite3"
    backup = tmp_path / "backups" / "hub-backup.sqlite3"

    live = sqlite3.connect(source)
    try:
        live.execute("PRAGMA journal_mode = WAL")
        live.execute("CREATE TABLE events(seq INTEGER PRIMARY KEY, value TEXT NOT NULL)")
        live.execute("INSERT INTO events(value) VALUES ('first'), ('second')")
        live.commit()

        created = backup_database(source, backup)
        assert created == backup.resolve()

        with sqlite3.connect(created) as restored:
            assert restored.execute("PRAGMA integrity_check").fetchone() == ("ok",)
            assert restored.execute("SELECT value FROM events ORDER BY seq").fetchall() == [
                ("first",),
                ("second",),
            ]
    finally:
        live.close()


def test_backup_refuses_overwrite_and_same_path(tmp_path: Path) -> None:
    source = tmp_path / "hub.sqlite3"
    with sqlite3.connect(source) as db:
        db.execute("CREATE TABLE state(value TEXT)")

    with pytest.raises(ValueError):
        backup_database(source, source)

    destination = tmp_path / "backup.sqlite3"
    destination.write_bytes(b"existing")
    with pytest.raises(FileExistsError):
        backup_database(source, destination)
    assert destination.read_bytes() == b"existing"
