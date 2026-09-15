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
from .zcode_sqlite import ZCodeSQLiteAdapter
from ..models import TaskChanges, TaskSummary, ZCodeSummary

_RUNNING = {"running", "in_progress", "inprogress", "active", "working", "executing"}
_WAITING = {"waiting", "queued", "pending", "ready", "todo"}
_FAILED = {"failed", "failure", "error", "errored"}
_COMPLETED = {"completed", "complete", "done", "success", "succeeded", "finished"}

_TARGET_REQUIRED = {
    "session_id",
    "objective",
    "status",
    "time_used_seconds",
    "time_updated",
    "summary_title",
    "active_run_started_at",
    "active_run_last_seen_at",
}
_TODO_REQUIRED = {"session_id", "content", "status", "position"}
_SESSION_REQUIRED = {
    "id",
    "directory",
    "path",
    "title",
    "summary_additions",
    "summary_deletions",
}


def _runtime_db_path() -> Path:
    configured = os.getenv("HUD_ZCODE_RUNTIME_DB")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".zcode" / "cli" / "db" / "db.sqlite"


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


def _timestamp(value: object) -> datetime | None:
    if value is None:
        return None
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if numeric <= 0:
        return None
    if numeric > 100_000_000_000_000:
        numeric /= 1_000_000
    elif numeric > 100_000_000_000:
        numeric /= 1_000
    try:
        return datetime.fromtimestamp(numeric, tz=timezone.utc)
    except (OverflowError, OSError, ValueError):
        return None


def _workspace_label(path: object, directory: object) -> str | None:
    for candidate in (path, directory):
        raw = "" if candidate is None else str(candidate).strip()
        if not raw:
            continue
        pieces = [piece for piece in re.split(r"[\\/]+", raw.rstrip("\\/")) if piece]
        if pieces:
            return pieces[-1][:500]
    return None


def _goal_id(session_id: object) -> str:
    digest = hashlib.sha256(str(session_id).encode("utf-8")).hexdigest()
    return f"zcode-goal-{digest[:32]}"


def _nonnegative_int(value: object) -> int | None:
    if value is None:
        return None
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        return None
    return parsed if parsed >= 0 else None


def _positive_seconds(name: str, default: int, maximum: int) -> int:
    raw = os.getenv(name)
    if raw is None:
        return default
    try:
        parsed = int(raw)
    except ValueError:
        return default
    return max(1, min(parsed, maximum))


class _GoalSchemaUnavailable(RuntimeError):
    pass


class ZCodeGoalAdapter:
    """Read the live Goal-mode state from ZCode's runtime SQLite database."""

    def __init__(
        self,
        db_path: Path | str | None = None,
        *,
        heartbeat_seconds: int | None = None,
        recent_terminal_seconds: int | None = None,
        task_limit: int = 20,
    ) -> None:
        self.db_path = Path(db_path).expanduser() if db_path is not None else _runtime_db_path()
        self.heartbeat_seconds = heartbeat_seconds or _positive_seconds(
            "HUD_ZCODE_GOAL_HEARTBEAT_SECONDS", 300, 3600
        )
        self.recent_terminal_seconds = recent_terminal_seconds or _positive_seconds(
            "HUD_ZCODE_GOAL_RECENT_TERMINAL_SECONDS", 1800, 86400
        )
        self.task_limit = max(1, min(task_limit, 100))

    @classmethod
    def from_environment(cls) -> "ZCodeGoalAdapter | None":
        path = _runtime_db_path()
        return cls(path) if path.is_file() else None

    async def collect_optional(self) -> ZCodePayload | None:
        return await asyncio.to_thread(self._collect_optional_sync)

    def _collect_optional_sync(self) -> ZCodePayload | None:
        if not self.db_path.is_file():
            return None

        path = self.db_path.resolve().as_posix()
        uri = f"file:{quote(path, safe='/:')}?mode=ro"
        try:
            connection = sqlite3.connect(uri, uri=True, timeout=1.0)
            connection.row_factory = sqlite3.Row
            try:
                self._verify_schema(connection)
                rows = connection.execute(
                    """
                    SELECT st.session_id, st.objective, st.status,
                           st.time_used_seconds, st.time_updated,
                           st.summary_title, st.active_run_started_at,
                           st.active_run_last_seen_at,
                           s.directory, s.path, s.title AS session_title,
                           s.summary_additions, s.summary_deletions
                    FROM session_target AS st
                    LEFT JOIN session AS s ON s.id = st.session_id
                    ORDER BY COALESCE(st.active_run_last_seen_at, st.time_updated) DESC
                    LIMIT ?
                    """,
                    (self.task_limit * 4,),
                ).fetchall()

                session_ids = [str(row["session_id"]) for row in rows]
                todos_by_session: dict[str, list[sqlite3.Row]] = {sid: [] for sid in session_ids}
                if session_ids:
                    placeholders = ",".join("?" for _ in session_ids)
                    todo_rows = connection.execute(
                        f"""
                        SELECT session_id, content, status, position
                        FROM todo
                        WHERE session_id IN ({placeholders})
                        ORDER BY session_id, position
                        """,
                        session_ids,
                    ).fetchall()
                    for todo in todo_rows:
                        todos_by_session.setdefault(str(todo["session_id"]), []).append(todo)
            finally:
                connection.close()
        except _GoalSchemaUnavailable:
            return None
        except sqlite3.Error as error:
            raise PublicAdapterError("ZCode live Goal database read failed") from error

        now = datetime.now(timezone.utc)
        tasks: list[TaskSummary] = []
        for row in rows:
            session_id = str(row["session_id"])
            todos = todos_by_session.get(session_id, [])
            task = self._to_task(row, todos, now)
            if task is not None:
                tasks.append(task)
            if len(tasks) >= self.task_limit:
                break

        if not tasks:
            return None

        counts = {"running": 0, "waiting": 0, "failed": 0, "completed": 0}
        for task in tasks:
            if task.status in counts:
                counts[task.status] += 1
        return ZCodePayload(summary=ZCodeSummary(**counts), tasks=tasks)

    @staticmethod
    def _verify_schema(connection: sqlite3.Connection) -> None:
        def columns(table: str) -> set[str]:
            exists = connection.execute(
                "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?",
                (table,),
            ).fetchone()
            if exists is None:
                raise _GoalSchemaUnavailable(table)
            return {row[1] for row in connection.execute(f'PRAGMA table_info("{table}")').fetchall()}

        if not _TARGET_REQUIRED.issubset(columns("session_target")):
            raise _GoalSchemaUnavailable("session_target")
        if not _TODO_REQUIRED.issubset(columns("todo")):
            raise _GoalSchemaUnavailable("todo")
        if not _SESSION_REQUIRED.issubset(columns("session")):
            raise _GoalSchemaUnavailable("session")

    def _to_task(
        self,
        row: sqlite3.Row,
        todos: list[sqlite3.Row],
        now: datetime,
    ) -> TaskSummary | None:
        heartbeat = _timestamp(row["active_run_last_seen_at"])
        updated_at = heartbeat or _timestamp(row["time_updated"])
        started_at = _timestamp(row["active_run_started_at"])
        heartbeat_fresh = (
            heartbeat is not None
            and max(0.0, (now - heartbeat).total_seconds()) <= self.heartbeat_seconds
        )

        todo_statuses = [_normalize_status(todo["status"]) for todo in todos]
        target_status = _normalize_status(row["status"])

        if "running" in todo_statuses or heartbeat_fresh or target_status == "running":
            status = "running"
        elif target_status == "failed":
            status = "failed"
        elif "waiting" in todo_statuses or target_status == "waiting":
            status = "waiting"
        elif todos and all(status_value == "completed" for status_value in todo_statuses):
            status = "completed"
        elif target_status == "completed":
            status = "completed"
        else:
            status = "unknown"

        recent_terminal = (
            status in {"failed", "completed"}
            and updated_at is not None
            and max(0.0, (now - updated_at).total_seconds()) <= self.recent_terminal_seconds
        )
        if status not in {"running", "waiting"} and not heartbeat_fresh and not recent_terminal:
            return None

        activity: str | None = None
        for wanted in ("running", "waiting"):
            for todo, todo_status in zip(todos, todo_statuses):
                if todo_status == wanted:
                    text = str(todo["content"]).strip()
                    if text:
                        activity = text[:500]
                        break
            if activity:
                break

        title = None
        for candidate in (row["session_title"], row["summary_title"], row["objective"]):
            text = "" if candidate is None else str(candidate).strip()
            if text:
                title = text[:500]
                break
        if title is None:
            title = "Goal mode"

        additions = _nonnegative_int(row["summary_additions"])
        deletions = _nonnegative_int(row["summary_deletions"])
        changes = (
            TaskChanges(additions=additions, deletions=deletions)
            if additions is not None or deletions is not None
            else None
        )

        duration_seconds = _nonnegative_int(row["time_used_seconds"])
        if duration_seconds is None and started_at is not None:
            duration_seconds = max(0, int((now - started_at).total_seconds()))

        return TaskSummary(
            id=_goal_id(row["session_id"]),
            title=title,
            workspace=_workspace_label(row["path"], row["directory"]),
            status=status,
            started_at=started_at,
            updated_at=updated_at,
            duration_seconds=duration_seconds,
            activity=activity,
            changes=changes,
        )


class ZCodeCompositeAdapter:
    """Prefer live Goal state; fall back to the best-effort task index."""

    def __init__(
        self,
        goal_adapter: ZCodeGoalAdapter | None,
        task_index_adapter: ZCodeSQLiteAdapter | None,
    ) -> None:
        self.goal_adapter = goal_adapter
        self.task_index_adapter = task_index_adapter

    @classmethod
    def from_environment(cls) -> "ZCodeCompositeAdapter | None":
        goal = ZCodeGoalAdapter.from_environment()
        task_index = ZCodeSQLiteAdapter.from_environment()
        if goal is None and task_index is None:
            return None
        return cls(goal, task_index)

    async def collect(self) -> ZCodePayload:
        if self.goal_adapter is not None:
            live = await self.goal_adapter.collect_optional()
            if live is not None and live.tasks:
                return live
        if self.task_index_adapter is not None:
            return await self.task_index_adapter.collect()
        raise PublicAdapterError("ZCode task sources are unavailable")
