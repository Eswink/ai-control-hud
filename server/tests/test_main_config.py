from __future__ import annotations

from server.__main__ import _listen_host, _listen_port


def test_listen_defaults(monkeypatch) -> None:
    monkeypatch.delenv("HUD_HOST", raising=False)
    monkeypatch.delenv("HUD_PORT", raising=False)
    assert _listen_host() == "0.0.0.0"
    assert _listen_port() == 8787


def test_listen_overrides_for_isolated_shadow(monkeypatch) -> None:
    monkeypatch.setenv("HUD_HOST", "127.0.0.1")
    monkeypatch.setenv("HUD_PORT", "8797")
    assert _listen_host() == "127.0.0.1"
    assert _listen_port() == 8797


def test_invalid_port_falls_back(monkeypatch) -> None:
    monkeypatch.setenv("HUD_PORT", "not-a-port")
    assert _listen_port() == 8787
    monkeypatch.setenv("HUD_PORT", "70000")
    assert _listen_port() == 8787
