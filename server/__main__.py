from __future__ import annotations

import os

import uvicorn

from .app import app


def _adapter_name(value: object | None) -> str:
    return "disabled" if value is None else type(value).__name__


if __name__ == "__main__":
    runtime = app.state.runtime
    mode = "fixture" if os.getenv("HUD_FIXTURE") else "production"
    print(f"[hud] mode={mode}")
    print(
        "[hud] adapters "
        f"zcode={_adapter_name(runtime.zcode_adapter)} "
        f"commandCode={_adapter_name(runtime.command_code_adapter)}"
    )
    print("[hud] listen=http://0.0.0.0:8787")
    uvicorn.run(app, host="0.0.0.0", port=8787, reload=False)
