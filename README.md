# AI Control HUD

A lightweight always-on HUD for monitoring AI coding-agent activity from an old Android device, with a 24/7 Central Hub between the development machine and the phone.

## Current architecture

```text
Windows development machine              24/7 CentOS server                  Android HUD
┌──────────────────────────┐             ┌────────────────────────────┐       ┌──────────────────────┐
│ ZCode / CommandCode      │             │ ai-control-hub (Go)        │       │ Native Java HUD      │
│          │               │   outbound  │                            │ HTTP  │                      │
│          ▼               ├────────────►│ latest canonical snapshot  ├──────►│ state dashboard      │
│ ai-control-agent (Go)    │             │ agent heartbeat            │       │ durable event cursor │
│          │               │             │ durable event log          │       │ local TextToSpeech   │
│          ├ local API     │             │ SQLite                     │       │ quiet hours          │
│          └ durable outbox│    UDP      │ LAN discovery :8788        │       │                      │
└──────────────────────────┘             └────────────────────────────┘       └──────────────────────┘
```

The production path is Go end-to-end on Windows and CentOS. The Android application remains native Java/XML.

The earlier Python/FastAPI Hub implementation remains in the repository temporarily as a protocol/parity reference while the Go Hub completes real-device cutover. New CentOS deployments should use the prebuilt `ai-control-hub` Go binary; they do not require Python, pip, a virtual environment, or a Go compiler.

## Working capabilities

### Windows Agent

- realtime ZCode Goal/session monitoring from local read-only SQLite state;
- freshness-bounded task-index fallback;
- CommandCode plan, credit, and usage-window collection;
- canonical schema-v1 state model;
- local `/api/v1/state` and `/api/v1/health` diagnostics;
- Windows service support;
- protected CommandCode credentials;
- protected Hub credentials using machine-scope DPAPI + ACL;
- outbound Hub snapshot/heartbeat upload with bounded timeout/backoff;
- durable SQLite task-event outbox with at-least-once delivery;
- LAN Hub auto-discovery and rediscovery.

### Central Hub

- standalone statically linked Go runtime;
- authenticated state, heartbeat, and event ingestion;
- SQLite persistence using the existing Central-Hub schema;
- schema-v1 Android-compatible `/api/v1/state` and `/api/v1/health`;
- independently versioned durable `/api/v1/events` cursor feed;
- stale/degraded projection when the Windows heartbeat disappears;
- globally idempotent `eventId` insertion and monotonic server `seq`;
- optional trusted-LAN UDP discovery on port 8788;
- hardened systemd deployment as an unprivileged user;
- live SQLite backup via the Go binary with integrity validation;
- token rotation without putting bearer tokens in command-line arguments.

### Android HUD

- native Java + XML Views, `minSdk` API 23;
- schema-v1 state dashboard;
- persistent event cursor;
- silent first-install baseline and Hub-reset rebase;
- local Android TextToSpeech for completed/failed tasks;
- independently configurable completed/failed speech;
- default quiet hours 23:00–08:00;
- old-event suppression and one catch-up summary;
- LAN Hub auto-discovery;
- dashboard display of the currently resolved physical Hub address;
- stable logical Hub identity across DHCP/IP changes.

## Security boundaries

- CommandCode/ZCode vendor credentials stay on the Windows development machine.
- The CentOS Hub and Android device receive only normalized operational state/events.
- Hub ingestion uses a separate project-internal bearer token; this token is **not** the CommandCode token.
- Android never receives the Hub ingestion token.
- Windows initiates outbound Hub connections; the Hub never polls Windows.
- UDP discovery is location discovery only, not authentication, and contains no token or private HUD state.
- Public Internet exposure is not part of the current deployment design.

## Development-machine Agent

The production collector lives in [`agent/`](agent/).

Local diagnostic endpoints:

```text
GET http://127.0.0.1:8787/api/v1/health
GET http://127.0.0.1:8787/api/v1/state
```

Typical foreground Windows launch:

```powershell
.\scripts\run-go-windows.ps1
```

Production Windows service and DPAPI credential details are documented in [`docs/G6_WINDOWS_SERVICE.md`](docs/G6_WINDOWS_SERVICE.md).

### Configure Central Hub upload

The preferred trusted-LAN mode stores a stable auto-discovery identity rather than a DHCP IP:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-auto `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

A successful status shows `url=auto://lan` plus the currently resolved physical URL. Manual `--hub-url http://<fixed-private-ip>:8787` remains available when broadcast discovery is blocked.

The Agent continues local collection/API behavior when the Hub or network is unavailable; terminal events remain in the durable outbox until delivery succeeds.

## Linux Central Hub — Go runtime

The production binary is:

```text
ai-control-hub
```

Commands:

```text
ai-control-hub version
ai-control-hub serve --host HOST --port PORT
ai-control-hub backup --database PATH --output PATH
```

Production configuration keeps the existing environment contract:

```text
HUD_HUB_AGENT_TOKEN=<project-internal agent bearer token>
HUD_HUB_AGENT_ID=desktop-main
HUD_HUB_DB=/var/lib/ai-control-hud/hub.sqlite3
HUD_HUB_STALE_AFTER_SECONDS=45
```

LAN-auto deployment adds Hub ID/discovery/HTTP metadata through the hardened systemd unit.

### Install on CentOS without Python

Use the CI-produced H3 validation/release bundle. After generating a new random Hub token file:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --binary "$PWD/ai-control-hub" \
  --token-file "$HOME/ai-control-hub.token" \
  --lan-auto \
  --hub-id dorm-hub \
  --agent-id desktop-main

bash scripts/ai-control-hub-systemd.sh start
```

The preferred LAN mode uses:

```text
TCP 8787   Hub HTTP API
UDP 8788   Hub discovery
```

The current DHCP address, such as `192.168.101.103`, is runtime/display information only and is not the stable client identity.

See [`docs/HUB_DEPLOYMENT.md`](docs/HUB_DEPLOYMENT.md) and [`docs/H3_FIELD_VALIDATION.md`](docs/H3_FIELD_VALIDATION.md).

## Hub API

Agent ingestion:

```text
POST /api/v1/agent/state
POST /api/v1/agent/heartbeat
POST /api/v1/agent/events
```

Android-compatible reads:

```text
GET /api/v1/state
GET /api/v1/health
GET /api/v1/events?after=<seq>&limit=<1..100>
```

Snapshots answer what is true now. Durable events answer what happened. They intentionally remain separate protocols.

## Android

Install the APK produced by Android CI.

Fresh installations use LAN auto-discovery. Existing installations can open `SERVER`, enter:

```text
auto.lan
```

and reconnect. In auto mode the dashboard displays the current physical address, for example:

```text
AUTO · http://192.168.101.103:8787
```

The stable internal identity remains `http://auto.lan`, so a DHCP IP change does not silently reset the durable event cursor.

## Data-source behavior

### ZCode

Realtime Goal mode is read from `~/.zcode/cli/db/db.sqlite` using verified session/target/todo metadata. A recent-heartbeat rule prevents historical sessions from being shown as running.

`~/.zcode/v2/tasks-index.sqlite` is only a fallback. Old task-index history is excluded by a freshness window so old failures do not appear as current HUD work.

### CommandCode

The Windows Agent calls the verified CommandCode billing source and normalizes plan/credit/5-hour/weekly state. Authentication, network, and schema failures remain explicit source errors/stale state rather than fabricated zero values.

## Technology constraints

### Windows Agent

- Go;
- pure-Go SQLite;
- local canonical SnapshotStore;
- local diagnostics + outbound Hub uploader + durable event outbox;
- platform-specific service/secret integrations isolated under platform packages.

### CentOS Hub

- standalone Go binary;
- pure-Go SQLite;
- no runtime Python dependency;
- no vendor credentials;
- no Redis/PostgreSQL/message broker until a measured requirement exists.

### Android

- native Android;
- Java + XML Views;
- no WebView/Compose/Flutter/React Native;
- `minSdk` API 23.

### CI

- Go Agent vet/test/build on Windows/Linux/macOS;
- dedicated Go Hub reproducible static Linux build + runtime/systemd/package smoke;
- Android lint/unit/APK CI;
- temporary Python Linux/Windows parity/reference tests until Go Hub field cutover is accepted.

## Project docs

- [`docs/HUB_V2_PLAN.md`](docs/HUB_V2_PLAN.md) — active Central Hub roadmap
- [`docs/GO_HUB_MIGRATION.md`](docs/GO_HUB_MIGRATION.md) — Go runtime migration/cutover contract
- [`docs/HUB_DEPLOYMENT.md`](docs/HUB_DEPLOYMENT.md) — Go Hub deployment/runbook
- [`docs/H3_FIELD_VALIDATION.md`](docs/H3_FIELD_VALIDATION.md) — real-device acceptance checklist
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/DECISIONS.md`](docs/DECISIONS.md)
- [`docs/API.md`](docs/API.md)
- [`docs/G6_WINDOWS_SERVICE.md`](docs/G6_WINDOWS_SERVICE.md)
- [`docs/ANDROID_IMPLEMENTATION.md`](docs/ANDROID_IMPLEMENTATION.md)
- [`docs/DEVICE_TEST.md`](docs/DEVICE_TEST.md)
- [`docs/sources/ZCODE.md`](docs/sources/ZCODE.md)
- [`docs/sources/COMMANDCODE.md`](docs/sources/COMMANDCODE.md)
