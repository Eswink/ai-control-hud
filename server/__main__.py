from __future__ import annotations

import os

import uvicorn

from .app import app


def _adapter_name(value: object | None) -> str:
    return "disabled" if value is None else type(value).__name__


def _listen_host() -> str:
    return os.getenv("HUD_HOST", "0.0.0.0").strip() or "0.0.0.0"


def _listen_port() -> int:
    raw = os.getenv("HUD_PORT", "8787").strip()
    try:
        port = int(raw)
    except ValueError:
        return 8787
    return port if 1 <= port <= 65535 else 8787


if __name__ == "__main__":
    runtime = app.state.runtime
    mode = "fixture" if os.getenv("HUD_FIXTURE") else "production"
    host = _listen_host()
    port = _listen_port()
    print(f"[hud] mode={mode}")
    print(
        "[hud] adapters "
        f"zcode={_adapter_name(runtime.zcode_adapter)} "
        f"commandCode={_adapter_name(runtime.command_code_adapter)}"
    )
    print(f"[hud] listen=http://{host}:{port}")
    uvicorn.run(app, host=host, port=port, reload=False)
