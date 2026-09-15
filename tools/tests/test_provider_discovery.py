from __future__ import annotations

import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

from tools.provider_discovery import build_report


def test_provider_report_scans_multiple_configs_without_secrets(tmp_path: Path) -> None:
    desktop = tmp_path / "v2-config.json"
    cli = tmp_path / "cli-config.json"
    desktop.write_text(
        json.dumps(
            {
                "provider": {
                    "builtin:zai": {
                        "name": "Z.ai",
                        "kind": "anthropic",
                        "enabled": True,
                        "options": {"baseURL": "https://api.z.ai/api/anthropic"},
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    cli.write_text(
        json.dumps(
            {
                "provider": {
                    "my-commandcode": {
                        "name": "CommandCode through ZCode",
                        "kind": "openai",
                        "enabled": True,
                        "options": {
                            "baseURL": "https://api.commandcode.ai/provider/v1?private=query",
                            "apiKey": "user_DO_NOT_SHARE_THIS_KEY",
                            "headers": {"Authorization": "Bearer ALSO_PRIVATE"},
                        },
                        "models": {"deepseek/deepseek-v4-flash": {}},
                    }
                }
            }
        ),
        encoding="utf-8",
    )

    report = build_report([desktop, cli])
    serialized = json.dumps(report)

    assert report["reportVersion"] == 2
    assert "user_DO_NOT_SHARE_THIS_KEY" not in serialized
    assert "Bearer ALSO_PRIVATE" not in serialized
    assert len(report["zcodeProviderConfigs"]) == 2
    candidate = report["commandCodeCandidates"][0]
    assert candidate["providerId"] == "my-commandcode"
    assert candidate["host"] == "api.commandcode.ai"
    assert candidate["baseURL"] == "https://api.commandcode.ai/provider/v1"
    assert candidate["apiKeyPresent"] is True
    assert candidate["modelIds"] == ["deepseek/deepseek-v4-flash"]


def test_runtime_report_correlates_only_active_goal_model_usage(tmp_path: Path) -> None:
    db = tmp_path / "db.sqlite"
    now_ms = int(datetime.now(timezone.utc).timestamp() * 1000)
    active_provider = "provider-active"
    historical_provider = "provider-historical"

    connection = sqlite3.connect(db)
    try:
        connection.execute(
            """
            CREATE TABLE session_target (
                session_id TEXT PRIMARY KEY,
                active_run_last_seen_at INTEGER
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE model_usage (
                id TEXT PRIMARY KEY,
                session_id TEXT NOT NULL,
                provider_id TEXT NOT NULL,
                model_id TEXT NOT NULL,
                status TEXT NOT NULL,
                started_at INTEGER NOT NULL
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE local_setting (
                scope TEXT NOT NULL,
                namespace TEXT NOT NULL,
                key TEXT NOT NULL,
                value TEXT NOT NULL,
                time_updated INTEGER NOT NULL
            )
            """
        )
        connection.executemany(
            "INSERT INTO session_target VALUES (?, ?)",
            [
                ("session-active", now_ms),
                ("session-old", now_ms - 86_400_000),
            ],
        )
        connection.executemany(
            "INSERT INTO model_usage VALUES (?, ?, ?, ?, ?, ?)",
            [
                ("u1", "session-active", active_provider, "model-current", "completed", now_ms - 2000),
                ("u2", "session-active", active_provider, "model-current", "completed", now_ms - 1000),
                ("u3", "session-old", historical_provider, "model-old", "completed", now_ms - 86_400_000),
            ],
        )
        connection.execute(
            "INSERT INTO local_setting VALUES (?, ?, ?, ?, ?)",
            ("user", "model", "reasoningLevel", "opaque-setting-value", now_ms),
        )
        connection.commit()
    finally:
        connection.close()

    report = build_report([], db)
    serialized = json.dumps(report)
    evidence = report["runtimeDbEvidence"]

    assert evidence["activeGoalModelUsage"] == [
        {
            "providerId": active_provider,
            "modelId": "model-current",
            "status": "completed",
            "requestCount": 2,
        }
    ]
    assert historical_provider in serialized
    assert "session-active" not in serialized
    assert "session-old" not in serialized
    assert "opaque-setting-value" not in serialized
