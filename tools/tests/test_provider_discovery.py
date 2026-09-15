from __future__ import annotations

import json
from pathlib import Path

from tools.provider_discovery import build_report


def test_provider_report_contains_connection_metadata_not_secrets(tmp_path: Path) -> None:
    config = tmp_path / "config.json"
    config.write_text(
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

    report = build_report(config)
    serialized = json.dumps(report)

    assert "user_DO_NOT_SHARE_THIS_KEY" not in serialized
    assert "Bearer ALSO_PRIVATE" not in serialized
    provider = report["zcodeProviderConfig"]["providers"][0]
    assert provider["providerId"] == "my-commandcode"
    assert provider["host"] == "api.commandcode.ai"
    assert provider["baseURL"] == "https://api.commandcode.ai/provider/v1"
    assert provider["apiKeyPresent"] is True
    assert provider["headerNames"] == ["Authorization"]
    assert provider["commandCodeCandidate"] is True
