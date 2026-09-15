from __future__ import annotations

import asyncio
import json
from pathlib import Path

import pytest

from server.hud.adapters.base import PublicAdapterError
from server.hud.adapters.commandcode_zcode import CommandCodeZCodeProviderAdapter


def _config(path: Path, *, base_url: str = "https://api.commandcode.ai/provider/v1") -> str:
    secret = "user_SUPER_SECRET"
    path.write_text(
        json.dumps(
            {
                "provider": {
                    "command-code": {
                        "enabled": True,
                        "kind": "openai",
                        "options": {"baseURL": base_url, "apiKey": secret},
                        "models": {"deepseek/deepseek-v4-flash": {}},
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    return secret


def test_collects_credits_windows_and_plan_from_zcode_key(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    secret = _config(path)
    calls: list[tuple[str, str]] = []

    def transport(endpoint: str, api_key: str) -> tuple[int, bytes]:
        calls.append((endpoint, api_key))
        if endpoint.endswith("/credits"):
            return 200, json.dumps(
                {
                    "credits": {"monthlyCredits": 53.72, "purchasedCredits": 5.0},
                    "windowLimits": {
                        "fiveHour": {"used": 11.34, "cap": 14.0, "resetAt": 1789490000000},
                        "weekly": {"used": 22.05, "cap": 35.0, "resetAt": "2026-09-19T06:00:00Z"},
                    },
                }
            ).encode()
        return 200, json.dumps(
            {"success": True, "data": {"planId": "pro", "status": "active"}}
        ).encode()

    payload = asyncio.run(
        CommandCodeZCodeProviderAdapter(path, transport=transport).collect()
    )

    assert calls == [
        ("/alpha/billing/credits", secret),
        ("/alpha/billing/subscriptions", secret),
    ]
    assert payload.usage.plan == "pro"
    assert payload.usage.credit is not None
    assert payload.usage.credit.remaining == pytest.approx(58.72)
    assert payload.usage.credit.limit is None
    assert payload.usage.credit.unit == "USD"
    assert [window.name for window in payload.usage.windows] == ["5h", "weekly"]
    assert payload.usage.windows[0].used_percent == pytest.approx(81.0)
    assert payload.usage.windows[0].reset_at is not None
    assert payload.usage.windows[1].used_percent == pytest.approx(63.0)


def test_subscription_failure_keeps_valid_credit_snapshot(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path)

    def transport(endpoint: str, _: str) -> tuple[int, bytes]:
        if endpoint.endswith("/credits"):
            return 200, json.dumps({"credits": {"monthlyCredits": 10}}).encode()
        return 503, b"service unavailable"

    payload = asyncio.run(
        CommandCodeZCodeProviderAdapter(path, transport=transport).collect()
    )
    assert payload.usage.plan is None
    assert payload.usage.credit is not None
    assert payload.usage.credit.remaining == 10


def test_credits_auth_failure_is_not_zero_balance(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path)

    adapter = CommandCodeZCodeProviderAdapter(
        path,
        transport=lambda _endpoint, _key: (401, b"unauthorized"),
    )
    with pytest.raises(PublicAdapterError, match="authentication failed"):
        asyncio.run(adapter.collect())


def test_malformed_credits_response_fails_explicitly(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path)

    adapter = CommandCodeZCodeProviderAdapter(
        path,
        transport=lambda _endpoint, _key: (200, b'{"unexpected": true}'),
    )
    with pytest.raises(PublicAdapterError, match="Unsupported CommandCode credits response"):
        asyncio.run(adapter.collect())


def test_missing_key_fails_without_leaking_provider_data(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    path.write_text(
        json.dumps(
            {
                "provider": {
                    "command-code": {
                        "options": {"baseURL": "https://api.commandcode.ai/provider/v1"},
                        "models": {"m": {}},
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    with pytest.raises(PublicAdapterError, match="API key missing"):
        asyncio.run(CommandCodeZCodeProviderAdapter(path).collect())


def test_nonofficial_provider_never_sends_key_without_explicit_opt_in(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path, base_url="http://127.0.0.1:7788/v1")
    calls: list[str] = []

    adapter = CommandCodeZCodeProviderAdapter(
        path,
        provider_id="command-code",
        transport=lambda endpoint, _key: (calls.append(endpoint) or 200, b"{}"),
    )
    with pytest.raises(PublicAdapterError, match="endpoint is not verified"):
        asyncio.run(adapter.collect())
    assert calls == []


def test_explicit_nonofficial_provider_can_reuse_verified_commandcode_key(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path, base_url="http://127.0.0.1:7788/v1")

    def transport(endpoint: str, _key: str) -> tuple[int, bytes]:
        if endpoint.endswith("/credits"):
            return 200, b'{"credits":{"monthlyCredits":3}}'
        return 503, b""

    payload = asyncio.run(
        CommandCodeZCodeProviderAdapter(
            path,
            provider_id="command-code",
            allow_nonofficial_provider=True,
            transport=transport,
        ).collect()
    )
    assert payload.usage.credit is not None
    assert payload.usage.credit.remaining == 3
