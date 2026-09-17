#!/usr/bin/env python3
"""Static architecture gate for the native Windows Agent UI."""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
UI = ROOT / "windows-ui"
SRC = UI / "src"


def fail(message: str) -> None:
    print(f"[windows-native-ui] FAIL {message}", file=sys.stderr)
    raise SystemExit(1)


def text(path: Path) -> str:
    if not path.is_file():
        fail(f"missing {path.relative_to(ROOT)}")
    return path.read_text(encoding="utf-8")


def main() -> int:
    cmake = text(UI / "CMakeLists.txt")
    joined = cmake + "\n" + "\n".join(
        path.read_text(encoding="utf-8")
        for path in sorted(SRC.glob("*.*"))
        if path.suffix.lower() in {".h", ".hpp", ".cpp", ".cc"}
    )
    lowered = joined.lower()
    banned = ("electron", "webview2", "wails", "qtwebengine", "react native", "flutter")
    for token in banned:
        if token in lowered:
            fail(f"web/heavy-shell dependency token is forbidden: {token}")

    required_cmake = ("cxx_std_20", "d2d1", "dwrite", "winhttp")
    for token in required_cmake:
        if token not in cmake:
            fail(f"CMake native contract is missing {token}")

    client = text(SRC / "agent_client.cpp")
    for token in ("WinHttpOpen", "127.0.0.1", "8787", "/api/v1/state", "/api/v1/diagnostics"):
        if token not in client:
            fail(f"Agent client is missing {token}")
    if "kMaxResponseBytes" not in client:
        fail("Agent client must bound local API response bytes")
    if "std::min<std::uint32_t>(tasks->Size(), 32)" not in client:
        fail("task rendering input must be bounded")

    app_header = text(SRC / "app.h")
    for token in ("std::jthread", "ID2D1HwndRenderTarget", "IDWriteFactory", "NOTIFYICONDATAW"):
        if token not in app_header:
            fail(f"native app shell is missing {token}")

    localization = text(SRC / "localization.cpp")
    if "简" not in localization and "本机 Agent" not in localization:
        fail("Simplified Chinese localization catalog is missing")

    print("[windows-native-ui] PASS native=C++20/Win32+D2D+DWrite transport=WinHTTP bounded=yes")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
