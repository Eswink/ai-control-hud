#!/usr/bin/env python3
"""Validate the release Hub binary's local storage-stats command."""
from __future__ import annotations

import argparse
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile


def create_fixture(path: Path) -> None:
    connection = sqlite3.connect(path)
    try:
        connection.executescript(
            """
            CREATE TABLE events (
                seq INTEGER PRIMARY KEY AUTOINCREMENT,
                event_id TEXT NOT NULL UNIQUE,
                agent_id TEXT NOT NULL,
                event_type TEXT NOT NULL,
                occurred_at TEXT NOT NULL,
                received_at TEXT NOT NULL,
                event_json TEXT NOT NULL
            );
            INSERT INTO events(event_id, agent_id, event_type, occurred_at, received_at, event_json)
            VALUES
              ('stats-event-1', 'desktop-main', 'task.completed',
               '2026-09-17T10:00:00+00:00', '2026-09-17T10:00:01+00:00', '{}'),
              ('stats-event-2', 'desktop-main', 'task.failed',
               '2026-09-17T11:00:00+00:00', '2026-09-17T11:00:01+00:00', '{}');
            """
        )
        connection.commit()
    finally:
        connection.close()


def run_smoke(binary: Path) -> None:
    binary = binary.resolve()
    with tempfile.TemporaryDirectory(prefix="ai-control-hub-stats-") as root:
        database = Path(root) / "hub.sqlite3"
        create_fixture(database)
        completed = subprocess.run(
            [str(binary), "stats", "--database", str(database)],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=10,
        )
        if completed.returncode != 0:
            raise RuntimeError(f"stats command failed: {completed.stderr.strip()}")
        output = completed.stdout
        required = (
            "[hub-stats] database=",
            "events=2 oldestSeq=1 latestSeq=2",
            "pageSize=",
            "freePages=",
            "oldestReceivedAt=2026-09-17T10:00:01Z",
            "newestReceivedAt=2026-09-17T11:00:01Z",
        )
        missing = [value for value in required if value not in output]
        if missing:
            raise RuntimeError(f"stats output missing {missing!r}: {output!r}")
        if "token" in output.lower() or "secret" in output.lower():
            raise RuntimeError("stats output unexpectedly contains credential-like text")
    print("[hub-stats-smoke] PASSED events=2 metadata=bounded credential_free=true")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path)
    args = parser.parse_args()
    try:
        run_smoke(args.hub)
    except (OSError, RuntimeError, subprocess.SubprocessError, sqlite3.Error) as exc:
        print(f"[hub-stats-smoke] FAILED {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
