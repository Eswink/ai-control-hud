from __future__ import annotations

import json
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
