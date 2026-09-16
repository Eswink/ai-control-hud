#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

from tools.hub_backup import backup_database


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Create an online-safe SQLite backup of the AI Control Hub database."
    )
    parser.add_argument("--database", required=True, type=Path, help="source hub.sqlite3")
    parser.add_argument("--output", required=True, type=Path, help="new backup file")
    args = parser.parse_args()

    created = backup_database(args.database, args.output)
    print(f"[backup] created={created}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
