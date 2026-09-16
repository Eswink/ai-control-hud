# AI Control HUD

A lightweight always-on control panel for monitoring AI coding-agent activity from an old Android device.

## Current status

The existing Windows + Android deployment is live and the repository is now migrating to **Central Hub V2** so the Android HUD remains useful when the development PC is powered off.

Working foundations:

- Go production agent on the development machine;
- realtime ZCode Goal/session monitoring from local read-only SQLite state;
- freshness-bounded task-index fallback;
- CommandCode plan, credit and usage-window collection;
- canonical schema-v1 `/api/v1/state` and `/api/v1/health`;
- native Android Java/XML HUD for the target old device;
- Windows service and protected local CommandCode credential storage;
- GitHub Actions builds/tests across the supported agent platforms and Android CI.

Central Hub V2 currently adds:

- a FastAPI + SQLite relay for the 24/7 Linux server;
- authenticated agent state and heartbeat ingestion;
- server-side last-seen freshness projection;
- preservation of the existing Android schema-v1 API;
- an optional outbound Go uploader with bounded timeouts/backoff.

Durable task events, Android TTS/quiet hours, and production Linux deployment hardening are the next iterations.

## Target architecture

```text
Development PC                       24/7 Linux server                 Old Android phone
┌──────────────────────┐             ┌───────────────────────┐        ┌────────────────────┐
│ ZCode / CommandCode  │             │ AI Control Hub        │        │ Native HUD         │
│         │            │  outbound   │ FastAPI + SQLite      │ HTTP   │ state + events     │
│         ▼            ├────────────►│ latest snapshot       ├───────►│ local TTS policy   │
│ Go ai-control-agent  │             │ heartbeat / events    │        │ quiet hours        │
│ + local diagnostics  │             │ schema-v1 API         │        │                    │
└──────────────────────┘             └───────────────────────┘        └────────────────────┘
```

Vendor credentials stay on the development machine. The Linux hub and Android device receive only normalized operational state/events.

See [`docs/HUB_V2_PLAN.md`](docs/HUB_V2_PLAN.md) for the active migration roadmap and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for boundaries/failure semantics.

## Development-machine agent

The production collector lives in [`agent/`](agent/). Local diagnostic endpoints remain available even when remote hub upload is enabled:

```text
GET http://127.0.0.1:8787/api/v1/health
GET http://127.0.0.1:8787/api/v1/state
```

Typical foreground launch on Windows:

```powershell
.\scripts\run-go-windows.ps1
```

The installed Windows service path and DPAPI-backed CommandCode credential flow are documented in [`docs/G6_WINDOWS_SERVICE.md`](docs/G6_WINDOWS_SERVICE.md).

### Optional Central Hub upload (H2 development configuration)

Remote upload is disabled by default. The current development slice enables it with:

```text
AI_CONTROL_HUB_URL=https://your-private-hub.example
AI_CONTROL_HUB_AGENT_ID=desktop-main
AI_CONTROL_HUB_TOKEN=<agent bearer token>
```

The agent keeps local collection/API functionality if the hub or network is unavailable. The environment-variable token is temporary migration configuration; production service deployment will move it into protected machine storage.

## Linux Central Hub

The hub entry point is `server.hub_app:create_production_hub_app`.

Required/configurable environment:

```text
HUD_HUB_AGENT_TOKEN=<agent bearer token>
HUD_HUB_AGENT_ID=desktop-main
HUD_HUB_DB=/var/lib/ai-control-hud/hub.sqlite3
HUD_HUB_STALE_AFTER_SECONDS=45
```

A development launch from the repository root can use:

```bash
python -m uvicorn 'server.hub_app:create_production_hub_app' --factory --host 0.0.0.0 --port 8787
```

Agent ingest:

```text
POST /api/v1/agent/state
POST /api/v1/agent/heartbeat
```

Android-compatible reads:

```text
GET /api/v1/state
GET /api/v1/health
```

Use a trusted LAN/private overlay (for example Tailscale/WireGuard) and/or TLS. Direct public exposure is not part of the current design.

## Android

Install the APK produced by the `Android CI` workflow. During H3 the configured backend URL moves from the Windows machine to the 24/7 Linux hub; the schema remains v1 so the existing parser remains compatible.

The app intentionally remains native Android Java + XML Views with no WebView/Compose/Flutter/React Native. `minSdk` is API 23.

Planned notification behavior:

- durable task completion/failure events;
- local Android TextToSpeech;
- user-selectable speech;
- default quiet hours 23:00–08:00;
- suppression/summary of old events received after long offline periods.

## Data-source behavior

### ZCode

Realtime Goal mode is read from `~/.zcode/cli/db/db.sqlite` using verified session/target/todo metadata. A recent-heartbeat rule prevents historical sessions from being shown as running.

`~/.zcode/v2/tasks-index.sqlite` is only a fallback. Old task-index history is excluded by a freshness window so old failures do not appear as current HUD work.

### CommandCode

The development-machine agent calls the verified CommandCode billing source and normalizes plan/credit/5-hour/weekly state. Authentication, network, and schema failures remain explicit source errors/stale state rather than fabricated zero values.

## Technology constraints

### Development agent

- Go
- pure-Go SQLite access
- local canonical SnapshotStore
- local diagnostics + optional outbound hub uploader
- platform-specific service/secret integrations isolated under platform packages

### Linux hub

- Python 3
- FastAPI
- SQLite initially
- no vendor credentials
- no Redis/PostgreSQL/message broker until a measured requirement exists

### Android

- native Android
- Java + XML Views
- no WebView
- no Compose
- no Flutter/React Native
- `minSdk` API 23

### Build/test

- GitHub Actions
- Go vet/test/build across Windows/Linux/macOS
- Python tests on Windows/Linux
- Android CI produces installable debug APK artifacts

## Project docs

- [`docs/HUB_V2_PLAN.md`](docs/HUB_V2_PLAN.md) — active Central Hub migration plan
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/DECISIONS.md`](docs/DECISIONS.md)
- [`docs/API.md`](docs/API.md)
- [`docs/G6_WINDOWS_SERVICE.md`](docs/G6_WINDOWS_SERVICE.md)
- [`docs/ANDROID_IMPLEMENTATION.md`](docs/ANDROID_IMPLEMENTATION.md)
- [`docs/DEVICE_TEST.md`](docs/DEVICE_TEST.md)
- [`docs/sources/ZCODE.md`](docs/sources/ZCODE.md)
- [`docs/sources/COMMANDCODE.md`](docs/sources/COMMANDCODE.md)
