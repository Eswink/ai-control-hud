# AI Control HUD

A lightweight always-on control panel for monitoring local AI coding agents from an old Android device.

## Current status

The target Windows + Android deployment is live and validated end to end:

- realtime ZCode Goal/session monitoring from the local read-only runtime database;
- freshness-bounded task-index fallback when no Goal is active;
- CommandCode plan, remaining credits, 5-hour window and weekly window;
- canonical `/api/v1/state` and `/api/v1/health` endpoints;
- native Android Java/XML HUD tested on the target older device;
- GitHub Actions builds installable debug APK artifacts.

## Architecture

```text
ZCode runtime DB -------\
                         > Python state service ---- JSON/HTTP ---- Native Android HUD
CommandCode billing ----/                         \---- health ----/

GitHub Actions ---------------- build APK -------------------------^
```

Credentials stay on the Windows development machine. The Android app only receives normalized HUD state.

## Windows quick start

### 1. Python environment

From the repository root in PowerShell:

```powershell
python -m venv .venv
.\.venv\Scripts\python.exe -m pip install -e ".[dev]"
```

### 2. CommandCode provider mirror

ZCode 3.11.x can show a custom provider in Model Settings without exposing it in the provider JSON registries used by the HUD. The target deployment therefore keeps a small local mirror at:

```text
.local\commandcode-provider.json
```

`.local/` is gitignored. Never commit this file.

Expected shape:

```json
{
  "provider": {
    "command": {
      "name": "command",
      "kind": "openai",
      "enabled": true,
      "options": {
        "baseURL": "https://api.commandcode.ai/provider/v1",
        "apiKey": "<local CommandCode API key>"
      },
      "models": {
        "deepseek/deepseek-v4.1-flash": {},
        "deepseek/deepseek-v4-flash": {}
      }
    }
  }
}
```

Verify Git will ignore it:

```powershell
git check-ignore .local\commandcode-provider.json
```

### 3. Start the HUD backend

```powershell
.\scripts\run-windows.ps1
```

The launcher:

- clears fixture mode;
- uses `.local\commandcode-provider.json` when present;
- leaves CommandCode disabled rather than inventing data when the mirror is absent;
- auto-detects the local ZCode runtime sources;
- starts the server on port `8787`.

A configuration-only check is also available:

```powershell
.\scripts\run-windows.ps1 -CheckOnly
```

### 4. Verify state

```powershell
curl http://127.0.0.1:8787/api/v1/health
curl http://127.0.0.1:8787/api/v1/state
```

A healthy fully connected deployment reports:

```text
overall.status = live
zcode.health.status = ok
commandCode.health.status = ok
```

### 5. Android

Install the APK produced by the `Android CI` GitHub Actions workflow. On first launch, configure the backend URL using the Windows machine's LAN/Tailscale address, for example:

```text
http://192.168.1.50:8787
```

The phone does not need ZCode or CommandCode credentials.

## Data-source behavior

### ZCode

Realtime Goal mode is read from `~/.zcode/cli/db/db.sqlite` using `session`, `session_target` and `todo` metadata. A recent-heartbeat rule prevents historical sessions from being shown as running.

`~/.zcode/v2/tasks-index.sqlite` is only a fallback. Old task-index history is excluded by a freshness window so weeks-old failures do not appear as current HUD work.

### CommandCode

The backend calls CommandCode billing endpoints through an adapter isolated from the Android API. Usage is refreshed independently from Android polling and normalized into plan/credit/5-hour/weekly fields. Authentication, network and schema failures are surfaced as source errors/stale state rather than fabricated zero values.

## Technology constraints

### Server

- Python 3
- FastAPI
- read-only SQLite adapters
- independent source collectors and last-known-good state

### Android

- native Android
- Java + XML Views
- no WebView
- no Compose
- no Flutter/React Native
- `minSdk` API 23

### Build

- GitHub Actions
- Gradle wrapper committed to the repository
- no local Android SDK/Android Studio required

## Project docs

- [`docs/PLAN.md`](docs/PLAN.md)
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/API.md`](docs/API.md)
- [`docs/DECISIONS.md`](docs/DECISIONS.md)
- [`docs/DEVICE_TEST.md`](docs/DEVICE_TEST.md)
- [`docs/sources/ZCODE.md`](docs/sources/ZCODE.md)
- [`docs/sources/COMMANDCODE.md`](docs/sources/COMMANDCODE.md)
