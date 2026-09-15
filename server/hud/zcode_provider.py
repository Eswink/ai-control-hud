from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit, urlunsplit


class ZCodeProviderConfigError(RuntimeError):
    pass


@dataclass(frozen=True)
class ZCodeProviderRecord:
    provider_id: str
    name: str | None
    kind: str | None
    enabled: bool
    base_url: str | None
    api_key: str | None = field(default=None, repr=False)
    header_names: tuple[str, ...] = ()
    model_ids: tuple[str, ...] = ()

    @property
    def host(self) -> str | None:
        if not self.base_url:
            return None
        try:
            return (urlsplit(self.base_url).hostname or "").lower() or None
        except ValueError:
            return None

    @property
    def is_official_commandcode(self) -> bool:
        return self.host == "api.commandcode.ai"


def default_zcode_provider_config() -> Path:
    configured = os.getenv("HUD_ZCODE_CONFIG")
    if configured:
        return Path(configured).expanduser()
    return Path.home() / ".zcode" / "v2" / "config.json"


def _clean_string(value: Any, *, max_length: int = 500) -> str | None:
    if not isinstance(value, str):
        return None
    cleaned = value.strip()
    return cleaned[:max_length] if cleaned else None


def _model_ids(value: Any) -> tuple[str, ...]:
    if isinstance(value, dict):
        return tuple(str(key)[:200] for key in value.keys())
    if isinstance(value, list):
        result: list[str] = []
        for item in value:
            if isinstance(item, str):
                result.append(item[:200])
            elif isinstance(item, dict):
                model_id = _clean_string(item.get("id"), max_length=200)
                if model_id:
                    result.append(model_id)
        return tuple(result)
    return ()


def load_zcode_providers(path: Path | str | None = None) -> list[ZCodeProviderRecord]:
    config_path = Path(path).expanduser() if path is not None else default_zcode_provider_config()
    try:
        root = json.loads(config_path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return []
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ZCodeProviderConfigError("ZCode provider config could not be read") from exc

    providers = root.get("provider") if isinstance(root, dict) else None
    if providers is None:
        return []
    if not isinstance(providers, dict):
        raise ZCodeProviderConfigError("Unsupported ZCode provider config schema")

    records: list[ZCodeProviderRecord] = []
    for raw_id, raw in providers.items():
        if not isinstance(raw, dict):
            continue
        provider_id = str(raw_id)[:200]
        options = raw.get("options") if isinstance(raw.get("options"), dict) else {}
        headers = options.get("headers") if isinstance(options.get("headers"), dict) else {}
        enabled = raw.get("enabled") is not False and raw.get("systemDisabledReason") in (None, "")
        base_url = _clean_string(options.get("baseURL")) or _clean_string(raw.get("baseURL"))
        api_key = _clean_string(options.get("apiKey"), max_length=4096)
        records.append(
            ZCodeProviderRecord(
                provider_id=provider_id,
                name=_clean_string(raw.get("name"), max_length=200),
                kind=_clean_string(raw.get("kind"), max_length=64),
                enabled=enabled,
                base_url=base_url,
                api_key=api_key,
                header_names=tuple(sorted(str(key)[:120] for key in headers.keys())),
                model_ids=_model_ids(raw.get("models")),
            )
        )
    return records


def find_commandcode_provider(
    path: Path | str | None = None,
    *,
    explicit_provider_id: str | None = None,
) -> ZCodeProviderRecord | None:
    providers = load_zcode_providers(path)
    if explicit_provider_id:
        for provider in providers:
            if provider.provider_id == explicit_provider_id:
                return provider
        return None
    for provider in providers:
        if provider.enabled and provider.is_official_commandcode:
            return provider
    return None


def sanitize_base_url(value: str | None) -> str | None:
    if not value:
        return None
    try:
        parsed = urlsplit(value)
    except ValueError:
        return None
    if not parsed.scheme or not parsed.hostname:
        return None
    host = parsed.hostname.lower()
    if parsed.port:
        host = f"{host}:{parsed.port}"
    path = parsed.path.rstrip("/") or "/"
    return urlunsplit((parsed.scheme.lower(), host, path, "", ""))


def sanitized_provider_summary(path: Path | str | None = None) -> dict[str, Any]:
    config_path = Path(path).expanduser() if path is not None else default_zcode_provider_config()
    result: dict[str, Any] = {
        "path": _display_path(config_path),
        "exists": config_path.is_file(),
        "secretValuesIncluded": False,
        "providers": [],
    }
    if not config_path.is_file():
        return result
    try:
        providers = load_zcode_providers(config_path)
    except ZCodeProviderConfigError as exc:
        result["error"] = type(exc).__name__
        return result

    result["providers"] = [
        {
            "providerId": item.provider_id,
            "name": item.name,
            "kind": item.kind,
            "enabled": item.enabled,
            "baseURL": sanitize_base_url(item.base_url),
            "host": item.host,
            "apiKeyPresent": bool(item.api_key),
            "headerNames": list(item.header_names),
            "modelIds": list(item.model_ids),
            "commandCodeCandidate": item.is_official_commandcode,
        }
        for item in providers
    ]
    return result


def _display_path(path: Path) -> str:
    try:
        resolved = path.expanduser().resolve()
        home = Path.home().resolve()
        return "~/" + resolved.relative_to(home).as_posix()
    except (OSError, ValueError):
        return f"<external>/{path.name}"
