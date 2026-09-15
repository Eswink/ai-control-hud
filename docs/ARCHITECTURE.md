# Architecture

## Overview

```text
┌──────────────────────── Development machine ────────────────────────┐
│                                                                     │
│  ZCode local state            CommandCode local/auth/usage          │
│          │                               │                          │
│          ▼                               ▼                          │
│   ZCodeAdapter                     CommandCodeAdapter                │
│          │                               │                          │
│          └──────────────┬────────────────┘                          │
│                         ▼                                           │
│                 Canonical State Store                               │
│                         │                                           │
│                  Python HTTP API                                    │
│                         │                                           │
└─────────────────────────┼───────────────────────────────────────────┘
                          │ LAN / private overlay network
                          ▼
┌──────────────────────── Android device ─────────────────────────────┐
│                                                                     │
│  Native Java Activity                                               │
│     ├─ periodic state fetch                                         │
│     ├─ JSON parser                                                  │
│     ├─ freshness/offline logic                                      │
│     └─ XML/View dashboard                                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Boundary 1: vendor adapters

Vendor-specific paths, table names, CLI details, authentication details, and undocumented endpoint behavior exist only under `server/adapters/`.

Adapters emit canonical Python models. They must not leak raw credential data or vendor-internal payloads through the HTTP API.

### Adapter contract

Each adapter should expose conceptually:

```python
class Adapter:
    async def collect(self) -> AdapterResult:
        ...
```

`AdapterResult` contains:

- normalized data,
- observation timestamp,
- source health,
- optional non-secret diagnostic text.

An adapter exception is caught at the collector boundary and converted to source health state.

## Boundary 2: state store

The state store owns the most recent normalized snapshot. HTTP handlers never query vendor data directly.

Benefits:

- Android polling is cheap.
- CommandCode is not queried for every phone request.
- Slow/failing adapters are isolated from API latency.
- Multiple clients can be added later without increasing vendor load.

## Boundary 3: internal HTTP API

The API is versioned from the beginning:

```text
/api/v1/state
/api/v1/health
```

The Android client depends only on this contract.

For v1 the transport is periodic HTTP polling because it has predictable lifecycle behavior on an older Android device and is easy to recover after Wi-Fi interruption. SSE/WebSocket can be revisited only if measurements show a need.

## Android architecture

Keep the process intentionally small:

```text
MainActivity
  ├─ Settings/connection state
  ├─ Scheduled refresh executor
  ├─ StateClient
  ├─ StateParser
  └─ DashboardRenderer
```

Do not rebuild the view hierarchy on every refresh. Keep stable views and mutate their displayed values.

If dynamic task cards require recycling after measurement, add `RecyclerView`; do not add it preemptively.

## Security model

### Allowed on Android

- Backend URL.
- Last displayed non-secret state.
- Refresh preference.

### Not allowed on Android

- CommandCode authentication tokens.
- ZCode credentials.
- GitHub signing material.
- Raw private logs unless explicitly added in a future design.

### Repository

The repository must contain only sanitized examples. `.env`, auth files, databases, keystores, signing passwords, and local logs must be ignored.

## Freshness semantics

Every source has independent health:

- `ok`: most recent collection succeeded.
- `stale`: last successful value exists but age exceeds policy or current refresh failed.
- `error`: no trustworthy value is currently available.
- `disabled`: source intentionally disabled/configuration absent.

The aggregate HUD status must never report `LIVE` if a required source is stale/error.

## Failure isolation

Examples:

- ZCode stopped: usage panel continues to work; ZCode panel shows stale/offline.
- CommandCode token expired: ZCode panel continues to work; usage panel shows auth/error state.
- Python backend offline: Android shows backend OFFLINE and retains last display only as explicitly stale data.
- Schema mismatch: adapter reports unsupported schema rather than returning an empty task list.

## Network model

v1 assumes trusted LAN or a private overlay network. Public Internet exposure is out of scope.

Initial local HTTP is acceptable for a trusted LAN deployment if Android network security is explicitly configured for the chosen backend host. If remote access is added, use a private overlay and/or TLS rather than exposing the service directly.

## Performance budget

Target characteristics for the Android client:

- one foreground Activity,
- no WebView/browser engine,
- no continuous animation,
- small JSON snapshots,
- 2-second default polling,
- minimal object churn,
- incremental UI updates.

Actual CPU/memory/battery targets will be set after first-device profiling rather than guessed in advance.
