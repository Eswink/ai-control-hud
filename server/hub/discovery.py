from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass

DISCOVERY_REQUEST = b"AI_CONTROL_HUD_DISCOVER_V1"
DISCOVERY_SERVICE = "ai-control-hud"
DISCOVERY_SCHEMA_VERSION = 1


@dataclass(frozen=True)
class DiscoveryAnnouncement:
    hub_id: str
    scheme: str
    http_port: int
    hub_version: str

    def encode(self) -> bytes:
        payload = {
            "service": DISCOVERY_SERVICE,
            "schemaVersion": DISCOVERY_SCHEMA_VERSION,
            "hubId": self.hub_id,
            "scheme": self.scheme,
            "httpPort": self.http_port,
            "hubVersion": self.hub_version,
        }
        return json.dumps(payload, separators=(",", ":"), sort_keys=True).encode("utf-8")


class HubDiscoveryProtocol(asyncio.DatagramProtocol):
    def __init__(self, announcement: DiscoveryAnnouncement) -> None:
        self._announcement = announcement
        self._transport: asyncio.DatagramTransport | None = None

    def connection_made(self, transport: asyncio.BaseTransport) -> None:
        if isinstance(transport, asyncio.DatagramTransport):
            self._transport = transport

    def datagram_received(self, data: bytes, addr: tuple[str, int]) -> None:
        if data.strip() != DISCOVERY_REQUEST:
            return
        if self._transport is not None:
            self._transport.sendto(self._announcement.encode(), addr)

    def connection_lost(self, exc: Exception | None) -> None:
        self._transport = None
