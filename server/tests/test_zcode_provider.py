from __future__ import annotations

import json
from pathlib import Path

from server.hud.zcode_provider import (
    find_commandcode_provider,
    load_zcode_providers,
    sanitized_provider_summary,
)


def _write_config(path: Path) -> str:
    secret = "user_TOP_SECRET_COMMANDCODE_KEY"
    path.write_text(
        json.dumps(
            {
                "provider": {
                    "command-code": {
                        "name": "Command Code",
                        "kind": "openai",
                        "enabled": True,
                        "options": {
                            "baseURL": "https://api.commandcode.ai/provider/v1?should=drop",
                            "apiKey": secret,
                            "headers": {"X-Custom": "PRIVATE_HEADER_VALUE"},
                        },
                        "models": {
                            "deepseek/deepseek-v4-flash": {},
                            "moonshotai/Kimi-K3": {},
                        },
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    return secret


def test_loads_official_commandcode_provider_without_secret_repr(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    secret = _write_config(path)

    provider = find_commandcode_provider(path)

    assert provider is not None
    assert provider.provider_id == "command-code"
    assert provider.host == "api.commandcode.ai"
    assert provider.api_key == secret
    assert secret not in repr(provider)
    assert provider.model_ids == (
        "deepseek/deepseek-v4-flash",
        "moonshotai/Kimi-K3",
    )


def test_sanitized_summary_never_emits_key_or_header_value(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    secret = _write_config(path)

    summary = sanitized_provider_summary(path)
    serialized = json.dumps(summary)

    assert secret not in serialized
    assert "PRIVATE_HEADER_VALUE" not in serialized
    provider = summary["providers"][0]
    assert provider["apiKeyPresent"] is True
    assert provider["headerNames"] == ["X-Custom"]
    assert provider["baseURL"] == "https://api.commandcode.ai/provider/v1"
    assert provider["commandCodeCandidate"] is True


def test_explicit_provider_id_can_select_nonofficial_proxy(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    path.write_text(
        json.dumps(
            {
                "provider": {
                    "cc-proxy": {
                        "kind": "openai",
                        "options": {
                            "baseURL": "http://127.0.0.1:7788/v1",
                            "apiKey": "user_proxy_secret",
                        },
                        "models": {"model-x": {}},
                    }
                }
            }
        ),
        encoding="utf-8",
    )

    assert find_commandcode_provider(path) is None
    provider = find_commandcode_provider(path, explicit_provider_id="cc-proxy")
    assert provider is not None
    assert provider.host == "127.0.0.1"


def test_missing_provider_section_is_empty(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    path.write_text("{}", encoding="utf-8")
    assert load_zcode_providers(path) == []
