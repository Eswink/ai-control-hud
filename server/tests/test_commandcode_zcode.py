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


def test_collects_full_credits_windows_and_known_plan(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    secret = _config(path)
    calls: list[tuple[str, str]] = []

    def transport(endpoint: str, api_key: str) -> tuple[int, bytes]:
        calls.append((endpoint, api_key))
        if endpoint.endswith("/credits"):
            return 200, json.dumps(
                {
                    "credits": {
                        "monthlyCredits": 53.72,
                        "purchasedCredits": 5.0,
                        "freeCredits": 1.0,
                    },
                    "windowLimits": {
                        "fiveHour": {"used": 11.34, "cap": 14.0, "resetAt": 1789490000000},
                        "weekly": {"used": 22.05, "cap": 35.0, "resetAt": "2026-09-19T06:00:00Z"},
                    },
                }
            ).encode()
        return 200, json.dumps(
            {
                "success": True,
                "data": {
                    "planId": "individual-goat",
                    "status": "active",
                    "currentPeriodEnd": "2026-10-01T00:00:00Z",
                },
            }
        ).encode()

    payload = asyncio.run(
        CommandCodeZCodeProviderAdapter(path, transport=transport).collect()
    )

    assert calls == [
        ("/alpha/billing/credits", secret),
        ("/alpha/billing/subscriptions", secret),
    ]
    assert payload.usage.plan == "GOAT"
    assert payload.usage.credit is not None
    assert payload.usage.credit.remaining == pytest.approx(59.72)
    assert payload.usage.credit.limit == pytest.approx(70.0)
    assert payload.usage.credit.unit == "USD"
    assert [window.name for window in payload.usage.windows] == ["5h", "weekly"]
    assert payload.usage.windows[0].used_percent == pytest.approx(81.0)
    assert payload.usage.windows[0].reset_at is not None
    assert payload.usage.windows[1].used_percent == pytest.approx(63.0)


def test_extra_credit_can_exceed_plan_allowance_without_invalid_balance(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path)

    def transport(endpoint: str, _api_key: str) -> tuple[int, bytes]:
        if endpoint.endswith("/credits"):
            return 200, json.dumps(
                {
                    "credits": {
                        "monthlyCredits": 65.0,
                        "purchasedCredits": 10.0,
                        "freeCredits": 2.0,
                    }
                }
            ).encode()
        return 200, json.dumps(
            {"success": True, "data": {"planId": "individual-goat"}}
        ).encode()

    payload = asyncio.run(CommandCodeZCodeProviderAdapter(path, transport=transport).collect())

    assert payload.usage.plan == "GOAT"
    assert payload.usage.credit is not None
    assert payload.usage.credit.remaining == pytest.approx(77.0)
    assert payload.usage.credit.limit is None


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


def test_unknown_plan_is_preserved_without_fabricated_allowance(tmp_path: Path) -> None:
    path = tmp_path / "config.json"
    _config(path)

    def transport(endpoint: str, _: str) -> tuple[int, bytes]:
        if endpoint.endswith("/credits"):
            return 200, b'{"credits":{"monthlyCredits":4}}'
        return 200, b'{"success":true,"data":{"planId":"future-plan"}}'

    payload = asyncio.run(CommandCodeZCodeProviderAdapter(path, transport=transport).collect())

    assert payload.usage.plan == "future-plan"
    assert payload.usage.credit is not None
    assert payload.usage.credit.limit is None


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
