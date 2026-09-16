from __future__ import annotations

import os
import sqlite3
import uuid
from pathlib import Path


def backup_database(source: Path, destination: Path) -> Path:
    source = Path(source).expanduser().resolve()
    destination = Path(destination).expanduser().resolve()

    if source == destination:
        raise ValueError("backup destination must differ from the source database")
    if not source.is_file():
        raise FileNotFoundError(f"hub database does not exist: {source}")
    if destination.exists():
        raise FileExistsError(f"backup destination already exists: {destination}")

    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_name(
        f".{destination.name}.tmp-{os.getpid()}-{uuid.uuid4().hex}"
    )

    source_uri = f"file:{source.as_posix()}?mode=ro"
    source_db: sqlite3.Connection | None = None
    target_db: sqlite3.Connection | None = None
    try:
        source_db = sqlite3.connect(source_uri, uri=True, timeout=10)
        target_db = sqlite3.connect(str(temporary), timeout=10)
        source_db.backup(target_db)
        target_db.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        result = target_db.execute("PRAGMA integrity_check").fetchone()
        if result is None or result[0] != "ok":
            raise RuntimeError("SQLite integrity_check failed for backup")
        target_db.commit()
        target_db.close()
        target_db = None
        os.chmod(temporary, 0o600)

        with temporary.open("rb") as handle:
            os.fsync(handle.fileno())
        temporary.replace(destination)
        return destination
    finally:
        if target_db is not None:
            target_db.close()
        if source_db is not None:
            source_db.close()
        try:
            temporary.unlink(missing_ok=True)
        except OSError:
            pass
