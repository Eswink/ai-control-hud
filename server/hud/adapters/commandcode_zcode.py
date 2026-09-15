from __future__ import annotations

import asyncio
import http.client
import json
import math
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

from .base import CommandCodePayload, PublicAdapterError
from ..models import CreditBalance, UsageSummary, UsageWindow
from ..zcode_provider import (
    ZCodeProviderConfigError,
    default_zcode_provider_config,
    find_commandcode_provider,
)

BillingTransport = Callable[[str, str], tuple[int, bytes]]

_CREDITS_PATH = "/alpha/billing/credits"
_SUBSCRIPTIONS_PATH = "/alpha/billing/subscriptions"
_MAX_RESPONSE_BYTES = 1_000_000


def _number(value: Any) -> float | None:
    if isinstance(value, bool) or value is None:
        return None
    if isinstance(value, (int, float)):
        number = float(value)
    elif isinstance(value, str):
        try:
            number = float(value.strip())
        except ValueError:
            return None
    else:
        return None
    return number if math.isfinite(number) else None


def _timestamp(value: Any) -> datetime | None:
    number = _number(value)
    if number is not None and number > 0:
        seconds = number / 1000 if number > 10_000_000_000 else number
        try:
            return datetime.fromtimestamp(seconds, tz=timezone.utc)
        except (OverflowError, OSError, ValueError):
            return None
    if not isinstance(value, str):
        return None
    text = value.strip()
    if not text:
        return None
    try:
        return datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError:
        return None


def _usage_window(value: Any, name: str) -> UsageWindow | None:
    if not isinstance(value, dict):
        return None
    cap = _number(value.get("cap"))
    used = _number(value.get("used"))
    if cap is None or cap <= 0:
        return None
    used = max(0.0, used or 0.0)
    percent = min(100.0, (used / cap) * 100.0)
    return UsageWindow(name=name, used_percent=percent, reset_at=_timestamp(value.get("resetAt")))


def _decode_json(body: bytes, *, label: str) -> dict[str, Any]:
    try:
        value = json.loads(body.decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise PublicAdapterError(f"Unsupported CommandCode {label} response") from exc
    if not isinstance(value, dict):
        raise PublicAdapterError(f"Unsupported CommandCode {label} response")
    return value


def _default_transport(path: str, api_key: str) -> tuple[int, bytes]:
    connection = http.client.HTTPSConnection("api.commandcode.ai", timeout=8)
    try:
        connection.request(
            "GET",
            path,
            headers={
                "Authorization": f"Bearer {api_key}",
                "Accept": "application/json",
                "User-Agent": "ai-control-hud/0.1",
            },
        )
        response = connection.getresponse()
        body = response.read(_MAX_RESPONSE_BYTES + 1)
        if len(body) > _MAX_RESPONSE_BYTES:
            raise PublicAdapterError("CommandCode billing response too large")
        return response.status, body
    except PublicAdapterError:
        raise
    except (OSError, TimeoutError, http.client.HTTPException) as exc:
        raise PublicAdapterError("CommandCode billing request failed") from exc
    finally:
        connection.close()


class CommandCodeZCodeProviderAdapter:
    """Reuse the CommandCode API key already configured inside ZCode.

    The adapter deliberately keeps the billing endpoint private to this module.
    The Android/canonical API never sees provider credentials or vendor paths.
    """

    def __init__(
        self,
        config_path: Path | str | None = None,
        *,
        provider_id: str | None = None,
        allow_nonofficial_provider: bool = False,
        transport: BillingTransport | None = None,
    ) -> None:
        self.config_path = (
            Path(config_path).expanduser()
            if config_path is not None
            else default_zcode_provider_config()
        )
        self.provider_id = provider_id
        self.allow_nonofficial_provider = allow_nonofficial_provider
        self._transport = transport or _default_transport

    @classmethod
    def from_environment(cls) -> "CommandCodeZCodeProviderAdapter | None":
        path = default_zcode_provider_config()
        if not path.is_file():
            return None
        explicit = os.getenv("HUD_COMMANDCODE_PROVIDER_ID") or None
        try:
            provider = find_commandcode_provider(path, explicit_provider_id=explicit)
        except ZCodeProviderConfigError:
            # Return an adapter so the malformed config becomes a visible source error
            # instead of silently looking disabled.
            return cls(
                path,
                provider_id=explicit,
                allow_nonofficial_provider=bool(explicit),
            )
        if provider is None:
            return None
        return cls(
            path,
            provider_id=provider.provider_id,
            allow_nonofficial_provider=bool(explicit),
        )

    async def collect(self) -> CommandCodePayload:
        return await asyncio.to_thread(self._collect_sync)

    def _collect_sync(self) -> CommandCodePayload:
        try:
            provider = find_commandcode_provider(
                self.config_path,
                explicit_provider_id=self.provider_id,
            )
        except ZCodeProviderConfigError as exc:
            raise PublicAdapterError("ZCode provider config could not be read") from exc
        if provider is None:
            raise PublicAdapterError("CommandCode provider not found in ZCode")
        if not provider.api_key:
            raise PublicAdapterError("CommandCode provider API key missing in ZCode")
        if not provider.is_official_commandcode and not self.allow_nonofficial_provider:
            raise PublicAdapterError("CommandCode provider endpoint is not verified")

        credits_status, credits_body = self._transport(_CREDITS_PATH, provider.api_key)
        self._raise_for_status(credits_status)
        credits_root = _decode_json(credits_body, label="credits")
        usage = self._parse_credits(credits_root)

        plan: str | None = None
        try:
            subscription_status, subscription_body = self._transport(
                _SUBSCRIPTIONS_PATH,
                provider.api_key,
            )
            if subscription_status in (401, 403):
                # Credits already authenticated successfully; subscription enrichment
                # is not allowed to turn a valid snapshot into a fabricated auth error.
                subscription_status = 0
            if 200 <= subscription_status < 300:
                plan = self._parse_plan(_decode_json(subscription_body, label="subscription"))
        except PublicAdapterError:
            # Plan lookup is best-effort enrichment. Credits/windows remain authoritative.
            plan = None

        return CommandCodePayload(
            usage=UsageSummary(plan=plan, credit=usage.credit, windows=usage.windows)
        )

    @staticmethod
    def _raise_for_status(status: int) -> None:
        if status == 401:
            raise PublicAdapterError("CommandCode authentication failed")
        if status == 403:
            raise PublicAdapterError("CommandCode billing access denied")
        if not 200 <= status < 300:
            raise PublicAdapterError(f"CommandCode billing API unavailable ({status})")

    @staticmethod
    def _parse_credits(root: dict[str, Any]) -> UsageSummary:
        credits = root.get("credits")
        if not isinstance(credits, dict):
            raise PublicAdapterError("Unsupported CommandCode credits response")
        monthly = _number(credits.get("monthlyCredits"))
        if monthly is None or monthly < 0:
            raise PublicAdapterError("Unsupported CommandCode credits response")
        purchased = _number(credits.get("purchasedCredits")) or 0.0
        remaining = monthly + max(0.0, purchased)

        limits = root.get("windowLimits")
        if not isinstance(limits, dict):
            limits = credits.get("windowLimits") if isinstance(credits.get("windowLimits"), dict) else {}
        windows: list[UsageWindow] = []
        five_hour = _usage_window(limits.get("fiveHour"), "5h")
        weekly = _usage_window(limits.get("weekly"), "weekly")
        if five_hour is not None:
            windows.append(five_hour)
        if weekly is not None:
            windows.append(weekly)

        return UsageSummary(
            plan=None,
            credit=CreditBalance(remaining=remaining, limit=None, unit="USD"),
            windows=windows,
        )

    @staticmethod
    def _parse_plan(root: dict[str, Any]) -> str | None:
        if root.get("success") is False:
            return None
        data = root.get("data")
        if data is None:
            return None
        if not isinstance(data, dict):
            raise PublicAdapterError("Unsupported CommandCode subscription response")
        plan = data.get("planId")
        if not isinstance(plan, str) or not plan.strip():
            return None
        return plan.strip()[:128]
