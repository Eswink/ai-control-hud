from __future__ import annotations

import json
from pathlib import Path

import pytest

from server.hub.config import HubConfig
from server.hub.discovery import (
    DISCOVERY_REQUEST,
    DISCOVERY_SCHEMA_VERSION,
    DISCOVERY_SERVICE,
    DiscoveryAnnouncement,
    HubDiscoveryProtocol,
)


class FakeDatagramTransport:
    def __init__(self) -> None:
        self.sent: list[tuple[bytes, tuple[str, int]]] = []

    def sendto(self, data: bytes, addr: tuple[str, int]) -> None:
        self.sent.append((data, addr))


def test_discovery_announcement_is_vendor_neutral_and_has_no_secret() -> None:
    payload = json.loads(
        DiscoveryAnnouncement(
            hub_id="central-hub",
            scheme="http",
            http_port=8787,
            hub_version="test-version",
        ).encode()
    )

    assert payload == {
        "service": DISCOVERY_SERVICE,
        "schemaVersion": DISCOVERY_SCHEMA_VERSION,
        "hubId": "central-hub",
        "scheme": "http",
        "httpPort": 8787,
        "hubVersion": "test-version",
    }
    assert "token" not in payload
    assert "agentToken" not in payload


def test_discovery_protocol_only_replies_to_magic_request() -> None:
    protocol = HubDiscoveryProtocol(
        DiscoveryAnnouncement(
            hub_id="central-hub",
            scheme="http",
            http_port=8787,
            hub_version="test-version",
        )
    )
    transport = FakeDatagramTransport()
    protocol._transport = transport  # type: ignore[assignment]

    protocol.datagram_received(b"not-our-protocol", ("192.168.101.20", 50000))
    assert transport.sent == []

    protocol.datagram_received(DISCOVERY_REQUEST, ("192.168.101.20", 50000))
    assert len(transport.sent) == 1
    data, addr = transport.sent[0]
    assert addr == ("192.168.101.20", 50000)
    assert json.loads(data)["httpPort"] == 8787


def test_hub_config_parses_discovery_environment(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setenv("HUD_HUB_DB", str(tmp_path / "hub.sqlite3"))
    monkeypatch.setenv("HUD_HUB_AGENT_ID", "desktop-main")
    monkeypatch.setenv("HUD_HUB_AGENT_TOKEN", "x" * 32)
    monkeypatch.setenv("HUD_HUB_ID", "dorm-hub")
    monkeypatch.setenv("HUD_HUB_DISCOVERY_ENABLED", "true")
    monkeypatch.setenv("HUD_HUB_DISCOVERY_PORT", "8788")
    monkeypatch.setenv("HUD_HUB_HTTP_SCHEME", "http")
    monkeypatch.setenv("HUD_HUB_HTTP_PORT", "8787")

    config = HubConfig.from_env()

    assert config.hub_id == "dorm-hub"
    assert config.discovery_enabled is True
    assert config.discovery_port == 8788
    assert config.http_scheme == "http"
    assert config.http_port == 8787


def test_hub_config_rejects_invalid_discovery_port(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setenv("HUD_HUB_DB", str(tmp_path / "hub.sqlite3"))
    monkeypatch.setenv("HUD_HUB_AGENT_TOKEN", "x" * 32)
    monkeypatch.setenv("HUD_HUB_DISCOVERY_PORT", "70000")

    with pytest.raises(RuntimeError, match="HUD_HUB_DISCOVERY_PORT"):
        HubConfig.from_env()
