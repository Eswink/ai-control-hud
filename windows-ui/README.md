# AI Control HUD — native Windows Agent UI

This directory contains the interactive Windows dashboard for the local Go Agent.

## Architecture

The GUI is deliberately a separate per-user process:

```text
ai-control-agent.exe       Windows SCM service / collector / uploader
        ↑
        │ loopback HTTP only
        │ /api/v1/state
        │ /api/v1/health
        │ /api/v1/diagnostics
        │
ai-control-agent-ui.exe    interactive user session
```

Closing or crashing the UI does not stop the Agent service.

The production UI stack is intentionally native and small:

- C++20;
- Win32 window/message loop;
- Direct2D + DirectWrite rendering;
- WinHTTP loopback transport;
- Windows.Data.Json via the Windows SDK C++/WinRT projection;
- native SCM status query;
- native notification-area icon.

It does **not** embed Electron, WebView2, Wails, Qt WebEngine, Flutter, or React Native.

## Resource behavior

- Rendering is driven by `WM_PAINT`; there is no fixed-FPS animation/render loop.
- Visible dashboard polling is 2 seconds.
- Hidden/tray mode is designed to use a slower/lightweight health path.
- Local API responses are capped at 512 KiB.
- UI state keeps at most 32 tasks and 8 CommandCode usage windows.
- Direct2D/DirectWrite objects use `Microsoft::WRL::ComPtr`.
- WinHTTP request handles and service handles are closed deterministically.
- The polling worker is a `std::jthread` and is joined during UI shutdown.

Absolute working-set budgets and repeated window/handle leak gates are added in UI8 after the production window surface is stable.

## Build

On a current Windows SDK / Visual Studio Build Tools environment:

```powershell
cmake -S windows-ui -B build/windows-ui -A x64
cmake --build build/windows-ui --config Release --parallel
ctest --test-dir build/windows-ui -C Release --output-on-failure
```

The resulting application is:

```text
build/windows-ui/Release/ai-control-agent-ui.exe
```

The existing `AIControlHUD` Windows service must be installed/running for live data. The GUI itself does not require elevation for read-only monitoring.

## Scope by UI milestone

UI4 establishes the native shell, local state/diagnostics client, localization, tray lifecycle, and read-only dashboard.

UI5 expands the operational dashboard surfaces.

UI6 adds explicitly elevated service actions by delegating to the existing Go Agent CLI; the UI process itself remains unprivileged.

UI7 adds compact/Mini HUD mode and persistence.

UI8 adds release packaging and Windows USER/GDI/handle/private-bytes leak gates.
