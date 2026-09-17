#!/usr/bin/env python3
"""Static architecture, privilege-boundary and low-refresh gates for the native Windows Agent UI."""
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


def check_privileged_boundary() -> None:
    privileged = SRC / "privileged_actions.cpp"
    if not privileged.exists():
        return

    actions = text(privileged)
    required = (
        "PrivilegedAction::Start",
        "PrivilegedAction::Stop",
        "PrivilegedAction::Restart",
        "PrivilegedAction::Upgrade",
        'GetEnvironmentVariableW(L"ProgramFiles"',
        'L"AI Control HUD\\\\ai-control-agent.exe"',
        'std::wstring(L"service ") + verb',
        'lpVerb = L"runas"',
        "ShellExecuteExW",
        "GetOpenFileNameW",
        "SEE_MASK_NOCLOSEPROCESS",
    )
    for token in required:
        if token not in actions:
            fail(f"privileged action boundary is missing {token!r}")

    delegated_forbidden = (
        "CreateServiceW",
        "ChangeServiceConfig",
        "RegSetValue",
        "CryptProtectData",
        "commandcode.dpapi",
        "hub.dpapi",
    )
    for token in delegated_forbidden:
        if token in actions:
            fail(f"UI must delegate privileged state changes to the Go CLI, found {token}")

    lower_actions = actions.lower()
    for token in ("cmd.exe", "powershell", "_wsystem(", "system(", "createprocessw("):
        if token in lower_actions:
            fail(f"privileged helper must not expose a generic command execution path: {token}")

    app = text(SRC / "app.cpp")
    for token in ("RunPrivilegedCommand", "ConfirmPrivileged", "MB_YESNO", "MB_DEFBUTTON2"):
        if token not in app:
            fail(f"privileged UI is missing explicit confirmation token {token!r}")


def check_mini_hud_contract() -> None:
    state_path = SRC / "window_state.cpp"
    if not state_path.exists():
        return

    app = text(SRC / "app.cpp")
    model = text(SRC / "model.h")
    state = text(state_path)
    required_app = (
        "kTrayMiniHud",
        "HWND_TOPMOST",
        "ToggleMiniHud",
        "LoadWindowState",
        "SaveWindowState",
        "SPI_GETHIGHCONTRAST",
        "DisplayEquivalent(snapshot_, next)",
        "if (repaint && window_ != nullptr) PostMessageW",
    )
    for token in required_app:
        if token not in app:
            fail(f"Mini HUD/low-refresh contract is missing {token!r}")

    for token in ("uptimeSeconds", "durationSeconds", "return true"):
        if token not in model:
            fail(f"semantic repaint comparator is missing {token!r}")

    for token in ("LOCALAPPDATA", "native-ui-state.txt", "create_directories", "std::filesystem::rename"):
        if token not in state:
            fail(f"window persistence contract is missing {token!r}")

    for token in ("SetTimer(", "CreateTimerQueueTimer(", "DwmFlush("):
        if token in app:
            fail(f"fixed-rate render/animation primitive is forbidden: {token}")


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

    check_privileged_boundary()
    check_mini_hud_contract()
    print(
        "[windows-native-ui] PASS native=C++20/Win32+D2D+DWrite transport=WinHTTP "
        "bounded=yes uac=allowlisted-delegation repaint=semantic mini-hud=persistent"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
