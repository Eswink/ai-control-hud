from __future__ import annotations

import sqlite3
import threading
from datetime import datetime
from pathlib import Path

from server.hud.models import HudState

from .models import AgentEvent, HubEvent


class HubStore:
    def __init__(self, database_path: Path) -> None:
        self.database_path = database_path
        self.database_path.parent.mkdir(parents=True, exist_ok=True)
        self._lock = threading.RLock()
        self._connection = sqlite3.connect(
            str(database_path),
            check_same_thread=False,
            isolation_level=None,
        )
        self._connection.row_factory = sqlite3.Row
        with self._lock:
            self._connection.execute("PRAGMA foreign_keys = ON")
            if str(database_path) != ":memory:":
                self._connection.execute("PRAGMA journal_mode = WAL")
            self._connection.executescript(
                """
                CREATE TABLE IF NOT EXISTS agents (
                    agent_id TEXT PRIMARY KEY,
                    agent_version TEXT,
                    last_seen_at TEXT NOT NULL,
                    last_sent_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS snapshots (
                    agent_id TEXT PRIMARY KEY,
                    received_at TEXT NOT NULL,
                    sent_at TEXT NOT NULL,
                    state_json TEXT NOT NULL,
                    FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
                );

                CREATE TABLE IF NOT EXISTS events (
                    seq INTEGER PRIMARY KEY AUTOINCREMENT,
                    event_id TEXT NOT NULL UNIQUE,
                    agent_id TEXT NOT NULL,
                    event_type TEXT NOT NULL,
                    occurred_at TEXT NOT NULL,
                    received_at TEXT NOT NULL,
                    event_json TEXT NOT NULL,
                    FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
                );

                CREATE INDEX IF NOT EXISTS idx_events_agent_seq
                    ON events(agent_id, seq);
                """
            )

    def close(self) -> None:
        with self._lock:
            self._connection.close()

    def record_state(
        self,
        agent_id: str,
        sent_at: datetime,
        state: HudState,
        received_at: datetime,
    ) -> None:
        payload = state.model_dump_json(by_alias=True)
        with self._lock:
            self._connection.execute("BEGIN IMMEDIATE")
            try:
                self._upsert_agent(
                    agent_id=agent_id,
                    sent_at=sent_at,
                    received_at=received_at,
                    agent_version=None,
                )
                self._connection.execute(
                    """
                    INSERT INTO snapshots(agent_id, received_at, sent_at, state_json)
                    VALUES (?, ?, ?, ?)
                    ON CONFLICT(agent_id) DO UPDATE SET
                        received_at = excluded.received_at,
                        sent_at = excluded.sent_at,
                        state_json = excluded.state_json
                    """,
                    (
                        agent_id,
                        received_at.isoformat(),
                        sent_at.isoformat(),
                        payload,
                    ),
                )
                self._connection.execute("COMMIT")
            except Exception:
                self._connection.execute("ROLLBACK")
                raise

    def record_heartbeat(
        self,
        agent_id: str,
        sent_at: datetime,
        received_at: datetime,
        agent_version: str | None,
    ) -> None:
        with self._lock:
            self._upsert_agent(
                agent_id=agent_id,
                sent_at=sent_at,
                received_at=received_at,
                agent_version=agent_version,
            )

    def record_events(
        self,
        agent_id: str,
        sent_at: datetime,
        events: list[AgentEvent],
        received_at: datetime,
    ) -> tuple[int, int]:
        accepted = 0
        duplicates = 0
        with self._lock:
            self._connection.execute("BEGIN IMMEDIATE")
            try:
                # Any authenticated event delivery also proves that the agent was alive at
                # received_at. A normal heartbeat still provides the steady-state signal.
                self._upsert_agent(
                    agent_id=agent_id,
                    sent_at=sent_at,
                    received_at=received_at,
                    agent_version=None,
                )
                for event in events:
                    cursor = self._connection.execute(
                        """
                        INSERT OR IGNORE INTO events(
                            event_id,
                            agent_id,
                            event_type,
                            occurred_at,
                            received_at,
                            event_json
                        ) VALUES (?, ?, ?, ?, ?, ?)
                        """,
                        (
                            event.event_id,
                            agent_id,
                            event.type,
                            event.occurred_at.isoformat(),
                            received_at.isoformat(),
                            event.model_dump_json(by_alias=True),
                        ),
                    )
                    if cursor.rowcount == 1:
                        accepted += 1
                    else:
                        duplicates += 1
                self._connection.execute("COMMIT")
            except Exception:
                self._connection.execute("ROLLBACK")
                raise
        return accepted, duplicates

    def load_state(self, agent_id: str) -> tuple[HudState, datetime] | None:
        with self._lock:
            row = self._connection.execute(
                """
                SELECT snapshots.state_json, agents.last_seen_at
                FROM snapshots
                JOIN agents ON agents.agent_id = snapshots.agent_id
                WHERE snapshots.agent_id = ?
                """,
                (agent_id,),
            ).fetchone()
        if row is None:
            return None
        state = HudState.model_validate_json(row["state_json"])
        last_seen_at = datetime.fromisoformat(row["last_seen_at"])
        return state, last_seen_at

    def list_events(self, agent_id: str, after: int, limit: int) -> list[HubEvent]:
        with self._lock:
            rows = self._connection.execute(
                """
                SELECT seq, agent_id, received_at, event_json
                FROM events
                WHERE agent_id = ? AND seq > ?
                ORDER BY seq ASC
                LIMIT ?
                """,
                (agent_id, after, limit),
            ).fetchall()

        result: list[HubEvent] = []
        for row in rows:
            event = AgentEvent.model_validate_json(row["event_json"])
            result.append(
                HubEvent(
                    **event.model_dump(),
                    seq=row["seq"],
                    agent_id=row["agent_id"],
                    received_at=datetime.fromisoformat(row["received_at"]),
                )
            )
        return result

    def _upsert_agent(
        self,
        *,
        agent_id: str,
        sent_at: datetime,
        received_at: datetime,
        agent_version: str | None,
    ) -> None:
        self._connection.execute(
            """
            INSERT INTO agents(agent_id, agent_version, last_seen_at, last_sent_at)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(agent_id) DO UPDATE SET
                agent_version = COALESCE(excluded.agent_version, agents.agent_version),
                last_seen_at = excluded.last_seen_at,
                last_sent_at = excluded.last_sent_at
            """,
            (
                agent_id,
                agent_version,
                received_at.isoformat(),
                sent_at.isoformat(),
            ),
        )
