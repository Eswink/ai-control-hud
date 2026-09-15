from __future__ import annotations

import asyncio
import hashlib
import os
import re
import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote

from .base import PublicAdapterError, ZCodePayload
from ..models import TaskSummary, ZCodeSummary

_REQUIRED_COLUMNS = {
    "workspace_key",
    "workspace_path",
    "task_id",
    "title",
    "task_status",
    "updated_at",
    "pinned",
    "archived",
    "deleted",
}

_RUNNING = {"running", "in_progress", "inprogress", "active", "working", "executing"}
_WAITING = {"waiting", "queued", "pending", "ready"}
_FAILED = {"failed", "failure", "error", "errored"}
_COMPLETED = {"completed", "complete", "done", "success", "succeeded", "finished"}


def _default_db_path() -> Path:
    return Path.home() / ".zcode" / "v2" / "tasks-index.sqlite"


def _normalize_status(value: object) -> str:
    if value is None:
        return "unknown"
    normalized = re.sub(r"[\s-]+", "_", str(value).strip().lower())
    if normalized in _RUNNING:
        return "running"
    if normalized in _WAITING:
        return "waiting"
    if normalized in _FAILED:
        return "failed"
    if normalized in _COMPLETED:
        return "completed"
    return "unknown"


def _workspace_label(path: object, key: object) -> str | None:
    raw_path = "" if path is None else str(path).strip()
    if raw_path:
        pieces = [piece for piece in re.split(r"[\\/]+", raw_path.rstrip("\\/")) if piece]
        if pieces:
            return pieces[-1][:500]
    raw_key = "" if key is None else str(key).strip()
    return raw_key[:500] or None


def _canonical_id(workspace_key: object, task_id: object) -> str:
    workspace = "" if workspace_key is None else str(workspace_key)
    task = "" if task_id is None else str(task_id)
    digest = hashlib.sha256(f"{workspace}\0{task}".encode("utf-8")).hexdigest()
    return f"zcode-{digest[:32]}"


def _timestamp(value: object) -> datetime | None:
    if value is None:
        return None
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if numeric <= 0:
        return None

    # ZCode's schema exposes integer timestamps but the first discovery report did
    # not prove their unit. Accept normal Unix seconds/milliseconds/microseconds by
    # magnitude until the explicit value-level probe records the target unit.
    if numeric > 100_000_000_000_000:
        numeric /= 1_000_000
    elif numeric > 100_000_000_000:
        numeric /= 1_000
    try:
        return datetime.fromtimestamp(numeric, tz=timezone.utc)
    except (OverflowError, OSError, ValueError):
        return None


def _positive_limit(raw: str | None, default: int = 50) -> int:
    if raw is None:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return max(1, min(value, 500))


def _canonical_summary(status_rows: list[sqlite3.Row]) -> ZCodeSummary:
    counts = {"running": 0, "waiting": 0, "failed": 0, "completed": 0}
    for row in status_rows:
        canonical = _normalize_status(row["task_status"])
        if canonical in counts:
            counts[canonical] += int(row["count"])
    return ZCodeSummary(**counts)


class ZCodeSQLiteAdapter:
    """Read the ZCode v2 task index without modifying ZCode state."""

    def __init__(self, db_path: Path | str | None = None, *, task_limit: int | None = None):
        self.db_path = Path(db_path).expanduser() if db_path is not None else _default_db_path()
        self.task_limit = task_limit or _positive_limit(os.getenv("HUD_ZCODE_TASK_LIMIT"))

    @classmethod
    def from_environment(cls) -> "ZCodeSQLiteAdapter | None":
        configured = os.getenv("HUD_ZCODE_DB")
        if configured:
            return cls(configured)
        default = _default_db_path()
        return cls(default) if default.is_file() else None

    async def collect(self) -> ZCodePayload:
        return await asyncio.to_thread(self._collect_sync)

    def _collect_sync(self) -> ZCodePayload:
        if not self.db_path.is_file():
            raise PublicAdapterError("ZCode task index not found")

        path = self.db_path.resolve().as_posix()
        uri = f"file:{quote(path, safe='/:')}?mode=ro"
        try:
            connection = sqlite3.connect(uri, uri=True, timeout=1.0)
            connection.row_factory = sqlite3.Row
            try:
                self._verify_schema(connection)
                rows = connection.execute(
                    """
                    SELECT workspace_key, workspace_path, task_id, title,
                           task_status, updated_at, pinned, archived, deleted
                    FROM tasks
                    WHERE archived = 0 AND deleted = 0
                    ORDER BY pinned DESC, updated_at DESC
                    LIMIT ?
                    """,
                    (self.task_limit,),
                ).fetchall()
                status_rows = connection.execute(
                    """
                    SELECT task_status, COUNT(*) AS count
                    FROM tasks
                    WHERE archived = 0 AND deleted = 0
                    GROUP BY task_status
                    """
                ).fetchall()
            finally:
                connection.close()
        except PublicAdapterError:
            raise
        except sqlite3.Error as error:
            raise PublicAdapterError("ZCode task index read failed") from error

        return ZCodePayload(
            summary=_canonical_summary(status_rows),
            tasks=[self._row_to_task(row) for row in rows],
        )

    @staticmethod
    def _verify_schema(connection: sqlite3.Connection) -> None:
        table = connection.execute(
            "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'tasks'"
        ).fetchone()
        if table is None:
            raise PublicAdapterError("Unsupported ZCode task index schema")
        columns = {
            row[1]
            for row in connection.execute('PRAGMA table_info("tasks")').fetchall()
        }
        if not _REQUIRED_COLUMNS.issubset(columns):
            raise PublicAdapterError("Unsupported ZCode task index schema")

    @staticmethod
    def _row_to_task(row: sqlite3.Row) -> TaskSummary:
        title = str(row["title"]).strip() or "(untitled task)"
        return TaskSummary(
            id=_canonical_id(row["workspace_key"], row["task_id"]),
            title=title[:500],
            workspace=_workspace_label(row["workspace_path"], row["workspace_key"]),
            status=_normalize_status(row["task_status"]),
            started_at=None,
            updated_at=_timestamp(row["updated_at"]),
            duration_seconds=None,
            activity=None,
            changes=None,
        )
