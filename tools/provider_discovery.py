from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote

from server.hud.zcode_provider import (
    default_zcode_provider_configs,
    sanitized_provider_summaries,
)


def _runtime_db_path() -> Path:
    configured = os.getenv("HUD_ZCODE_RUNTIME_DB")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".zcode" / "cli" / "db" / "db.sqlite"


def _display_path(path: Path) -> str:
    try:
        resolved = path.expanduser().resolve()
        home = Path.home().resolve()
        return "~/" + resolved.relative_to(home).as_posix()
    except (OSError, ValueError):
        return f"<external>/{path.name}"


def _readonly_connection(path: Path) -> sqlite3.Connection:
    resolved = path.expanduser().resolve().as_posix()
    uri = f"file:{quote(resolved, safe='/:')}?mode=ro"
    connection = sqlite3.connect(uri, uri=True, timeout=1.0)
    connection.row_factory = sqlite3.Row
    return connection


def _columns(connection: sqlite3.Connection, table: str) -> set[str]:
    exists = connection.execute(
        "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)
    ).fetchone()
    if exists is None:
        return set()
    return {
        row[1]
        for row in connection.execute(f'PRAGMA table_info("{table}")').fetchall()
    }


def _timestamp_seconds(value: object) -> float | None:
    if value is None:
        return None
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if numeric <= 0:
        return None
    if numeric > 100_000_000_000_000:
        return numeric / 1_000_000
    if numeric > 100_000_000_000:
        return numeric / 1_000
    return numeric


def _active_goal_session(connection: sqlite3.Connection) -> str | None:
    target_columns = _columns(connection, "session_target")
    if not {"session_id", "active_run_last_seen_at"}.issubset(target_columns):
        return None
    row = connection.execute(
        """
        SELECT session_id, active_run_last_seen_at
        FROM session_target
        WHERE active_run_last_seen_at IS NOT NULL
        ORDER BY active_run_last_seen_at DESC
        LIMIT 1
        """
    ).fetchone()
    if row is None:
        return None
    last_seen = _timestamp_seconds(row["active_run_last_seen_at"])
    if last_seen is None:
        return None
    if max(0.0, datetime.now(timezone.utc).timestamp() - last_seen) > 10 * 60:
        return None
    return str(row["session_id"])


def runtime_db_evidence(path: Path) -> dict:
    result = {
        "path": _display_path(path),
        "exists": path.is_file(),
        "readOnly": True,
        "settingValuesIncluded": False,
        "sessionIdsIncluded": False,
        "promptOrMessageContentIncluded": False,
        "localSettings": [],
        "recentModelUsage": [],
        "activeGoalModelUsage": [],
    }
    if not path.is_file():
        return result

    try:
        connection = _readonly_connection(path)
        try:
            setting_columns = _columns(connection, "local_setting")
            if {"scope", "namespace", "key", "value", "time_updated"}.issubset(
                setting_columns
            ):
                setting_rows = connection.execute(
                    """
                    SELECT scope, namespace, key,
                           typeof(value) AS value_type,
                           length(value) AS value_length
                    FROM local_setting
                    WHERE lower(namespace) LIKE '%model%'
                       OR lower(namespace) LIKE '%provider%'
                       OR lower(key) LIKE '%model%'
                       OR lower(key) LIKE '%provider%'
                       OR lower(key) LIKE '%endpoint%'
                       OR lower(key) LIKE '%baseurl%'
                    ORDER BY time_updated DESC
                    LIMIT 100
                    """
                ).fetchall()
                result["localSettings"] = [
                    {
                        "scope": str(row["scope"])[:80],
                        "namespace": str(row["namespace"])[:120],
                        "key": str(row["key"])[:160],
                        "valueType": str(row["value_type"])[:40],
                        "valueLength": int(row["value_length"] or 0),
                    }
                    for row in setting_rows
                ]

            usage_columns = _columns(connection, "model_usage")
            required_usage = {"session_id", "provider_id", "model_id", "status", "started_at"}
            if required_usage.issubset(usage_columns):
                usage_rows = connection.execute(
                    """
                    SELECT provider_id, model_id, status,
                           COUNT(*) AS request_count,
                           MAX(started_at) AS latest_started_at
                    FROM model_usage
                    GROUP BY provider_id, model_id, status
                    ORDER BY latest_started_at DESC
                    LIMIT 30
                    """
                ).fetchall()
                result["recentModelUsage"] = [
                    {
                        "providerId": str(row["provider_id"])[:300],
                        "modelId": str(row["model_id"])[:300],
                        "status": str(row["status"])[:80],
                        "requestCount": int(row["request_count"]),
                    }
                    for row in usage_rows
                ]

                active_session = _active_goal_session(connection)
                if active_session is not None:
                    active_rows = connection.execute(
                        """
                        SELECT provider_id, model_id, status,
                               COUNT(*) AS request_count,
                               MAX(started_at) AS latest_started_at
                        FROM model_usage
                        WHERE session_id = ?
                        GROUP BY provider_id, model_id, status
                        ORDER BY latest_started_at DESC
                        LIMIT 20
                        """,
                        (active_session,),
                    ).fetchall()
                    result["activeGoalModelUsage"] = [
                        {
                            "providerId": str(row["provider_id"])[:300],
                            "modelId": str(row["model_id"])[:300],
                            "status": str(row["status"])[:80],
                            "requestCount": int(row["request_count"]),
                        }
                        for row in active_rows
                    ]
        finally:
            connection.close()
    except (OSError, sqlite3.Error) as exc:
        result["error"] = type(exc).__name__
    return result


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate a secret-free summary of ZCode model providers for AI Control HUD."
    )
    parser.add_argument(
        "--config",
        action="append",
        default=None,
        help=(
            "ZCode provider config path. Repeatable. Default scans both "
            "~/.zcode/v2/config.json and ~/.zcode/cli/config.json."
        ),
    )
    parser.add_argument(
        "--runtime-db",
        default=str(_runtime_db_path()),
        help="ZCode runtime SQLite DB for sanitized provider/model evidence.",
    )
    parser.add_argument(
        "--output",
        default=".local/provider-discovery.json",
        help="Output path (default is gitignored).",
    )
    return parser.parse_args(argv)


def build_report(
    configs: list[Path] | tuple[Path, ...],
    runtime_db: Path | None = None,
) -> dict:
    summaries = sanitized_provider_summaries(configs)
    candidates = []
    for summary in summaries:
        for provider in summary.get("providers", []):
            if provider.get("commandCodeCandidate"):
                candidates.append(
                    {
                        "sourcePath": summary.get("path"),
                        "providerId": provider.get("providerId"),
                        "name": provider.get("name"),
                        "kind": provider.get("kind"),
                        "baseURL": provider.get("baseURL"),
                        "host": provider.get("host"),
                        "apiKeyPresent": provider.get("apiKeyPresent"),
                        "modelIds": provider.get("modelIds", []),
                    }
                )
    return {
        "reportVersion": 2,
        "generatedAt": datetime.now(timezone.utc).isoformat(),
        "privacy": {
            "networkCalls": False,
            "apiKeyValuesIncluded": False,
            "headerValuesIncluded": False,
            "settingValuesIncluded": False,
            "sessionIdsIncluded": False,
            "promptOrTaskContentIncluded": False,
            "usernameIncluded": False,
        },
        "zcodeProviderConfigs": summaries,
        "commandCodeCandidates": candidates,
        "runtimeDbEvidence": (
            runtime_db_evidence(runtime_db) if runtime_db is not None else None
        ),
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    configs = (
        [Path(value).expanduser() for value in args.config]
        if args.config
        else list(default_zcode_provider_configs())
    )
    report = build_report(configs, Path(args.runtime_db).expanduser())
    output = Path(args.output).expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Wrote sanitized provider report to {output}")
    print(
        "Scanned ZCode provider configs plus read-only runtime setting/model metadata. "
        "Setting values, session IDs and prompt/message content are not written."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
