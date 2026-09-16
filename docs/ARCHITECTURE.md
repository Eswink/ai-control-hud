# Architecture

## Current target

AI Control HUD is moving from a direct development-machine-to-Android topology to a relay topology with a 24/7 Linux authority.

```text
┌──────────────────────── Development machine ────────────────────────┐
│                                                                     │
│  ZCode local state            CommandCode local/auth/usage          │
│          │                               │                          │
│          ▼                               ▼                          │
│   Go ZCode collector              Go CommandCode collector           │
│          │                               │                          │
│          └──────────────┬────────────────┘                          │
│                         ▼                                           │
│                 Canonical SnapshotStore                             │
│                    │              │                                 │
│                    │              └── local schema-v1 HTTP API      │
│                    │                  (diagnostics / rollback)       │
│                    ▼                                                │
│              Remote uploader                                       │
│              state + heartbeat                                     │
└────────────────────┼────────────────────────────────────────────────┘
                     │ outbound only
                     │ trusted LAN / private overlay / TLS
                     ▼
┌──────────────────────── 24/7 Linux server ──────────────────────────┐
│                                                                     │
│                       AI Control Hub                                │
│                                                                     │
│   authenticated agent ingest                                       │
│          │                                                          │
│          ├── latest canonical snapshot ──┐                          │
│          ├── heartbeat / last-seen       ├── SQLite                 │
│          └── durable events (H4) ────────┘                          │
│                         │                                           │
│                  schema-v1 HTTP API                                 │
│                         │                                           │
└─────────────────────────┼───────────────────────────────────────────┘
                          │
                          ▼
┌──────────────────────── Android device ─────────────────────────────┐
│                                                                     │
│  Native Java Activity                                               │
│     ├─ state fetch                                                   │
│     ├─ event cursor (H4/H5)                                         │
│     ├─ freshness/offline rendering                                  │
│     ├─ local TextToSpeech policy (H5)                               │
│     └─ XML/View dashboard                                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

The detailed migration work order is maintained in [`HUB_V2_PLAN.md`](HUB_V2_PLAN.md).

## Boundary 1: local vendor collectors

ZCode and CommandCode vendor-specific paths, database schemas, authentication details, and undocumented provider behavior remain on the development machine under the Go agent.

Collectors emit canonical `domain.HudState` data. Raw vendor credentials and raw private payloads must never be sent to the hub or Android.

The existing Python adapters remain useful as legacy/reference implementations and tests, but the Go agent is the production local collector.

## Boundary 2: local canonical snapshot

`agent/internal/store.SnapshotStore` owns the latest canonical state on the development machine.

Both local HTTP diagnostics and remote upload read from this store. Neither path invokes vendor APIs directly.

Benefits:

- local diagnostics continue working if the hub is unavailable;
- remote upload cannot multiply vendor request load;
- collection cadence stays independent from Android refresh cadence;
- the hub receives one vendor-neutral schema.

## Boundary 3: outbound agent transport

The development machine initiates connections to the hub. The hub does not poll or open connections back into Windows.

Phase H2 transport consists of:

```text
POST /api/v1/agent/state
POST /api/v1/agent/heartbeat
```

Agent state and heartbeat uploads use bounded timeouts and retry/backoff. Hub/network failure is not fatal to local collection or the local diagnostic API.

The phase-H2 bearer token is transport authentication only. Production deployment must store it in protected machine storage rather than source control or command-line arguments.

## Boundary 4: central hub state

The Linux hub is the eventual Android authority. It persists:

- the latest accepted canonical state per agent;
- server receive-time heartbeat/last-seen metadata;
- durable ordered events in H4.

Server receive time, rather than the development machine clock, determines agent connectivity freshness.

For the primary agent the hub exposes the existing Android-compatible endpoints:

```text
GET /api/v1/state
GET /api/v1/health
```

If the primary agent disappears, the last trustworthy snapshot is retained but previously `ok` sources are projected as `stale` after the configured heartbeat threshold. The aggregate state becomes `degraded`; old valid task/usage data is not converted into empty/zero values.

## Boundary 5: snapshot versus event stream

Snapshot and event semantics are intentionally separate.

A snapshot answers **what is true now**. It is replaceable and only the latest value needs to be retained for the HUD state API.

An event answers **what happened**. Starting in H4, events are durable, ordered by a server sequence, and idempotent by `eventId`.

This distinction is required for reliable completion/failure notifications: Android must not depend on observing every intermediate snapshot transition.

Target event flow:

```text
collector state transition
        │
        ▼
agent durable outbox
        │ at-least-once
        ▼
hub UNIQUE(event_id) event log
        │ ordered seq cursor
        ▼
Android event consumer
```

## Android architecture

The client remains intentionally small:

```text
MainActivity
  ├─ Settings / connection state
  ├─ Scheduled state refresh
  ├─ StateClient / parser
  ├─ DashboardRenderer
  ├─ Event consumer (H4/H5)
  └─ TextToSpeech policy (H5)
```

Do not rebuild the view hierarchy on every refresh. Keep stable views and mutate displayed values.

Text-to-speech policy belongs on Android because the device owns user-facing interruption behavior. Planned defaults:

- completion/failure voice may be enabled;
- quiet hours default to 23:00–08:00;
- stale historical events are not spoken individually;
- server semantic events do not dictate volume or playback policy.

The dedicated foreground HUD remains the primary lifecycle model. FCM/background push is deferred unless real-device measurements show that screen-off delivery is required.

## Security model

### Development machine

Allowed:

- ZCode database paths;
- CommandCode provider credential in protected local storage;
- hub endpoint, agent ID, and protected hub credential.

The machine initiates outbound hub requests. No new inbound port is required for hub operation.

### Linux hub

Allowed:

- normalized canonical state;
- agent IDs and versions;
- heartbeat timestamps;
- normalized task events;
- agent authentication material stored outside Git.

Not allowed:

- CommandCode API keys;
- ZCode credentials;
- raw private vendor logs unless a later design explicitly introduces sanitized diagnostics.

### Android

Allowed:

- hub URL;
- last displayed non-secret state;
- event cursor;
- refresh and notification preferences.

Not allowed:

- CommandCode authentication tokens;
- ZCode credentials;
- GitHub signing material;
- agent ingest token;
- raw private logs.

### Repository

The repository contains sanitized examples only. `.env`, auth files, databases, keystores, signing passwords, deployment tokens, and local logs must remain ignored.

## Freshness semantics

Every vendor source retains the schema-v1 health model:

- `ok`: most recent collection succeeded and the agent is fresh;
- `stale`: last-known-good data exists but source refresh failed, source age exceeded policy, or the agent heartbeat expired;
- `error`: no trustworthy value is available;
- `disabled`: source intentionally disabled/configuration absent.

The aggregate HUD status must never report `live` if a required source is stale/error/disabled.

There are two freshness layers:

1. **collector freshness** — generated by the local agent for ZCode/CommandCode;
2. **agent connectivity freshness** — generated by the hub from server-observed heartbeat age.

The hub may downgrade `ok` to `stale`; it must not upgrade an agent-reported `stale`, `error`, or `disabled` source to `ok`.

## Failure isolation

Examples:

- ZCode stopped: CommandCode remains available; ZCode is stale/error.
- CommandCode auth expired: ZCode remains available; usage reports auth/error semantics.
- Hub unreachable from Windows: local collectors and local `/api/v1/state` continue operating; uploader retries with bounded backoff.
- Windows powered off: hub remains reachable and retains the last snapshot, then projects it stale after heartbeat expiry.
- Android loses network: it renders hub connectivity failure while retaining only explicitly stale last-displayed data.
- Hub restart: latest snapshot survives in SQLite.
- Schema mismatch: invalid/unsupported canonical state is rejected rather than guessed.

## Network model

The preferred deployment is a trusted LAN or private overlay such as Tailscale/WireGuard. Direct public exposure is out of scope.

Plain HTTP may be acceptable only when transport is already protected by the trusted/private network boundary. Otherwise use TLS because agent bearer credentials and operational metadata traverse the connection.

## Performance budget

### Development machine

- collector cadence remains independent of upload cadence;
- state upload is initially 5 seconds;
- heartbeat is initially 10 seconds;
- requests have bounded timeouts;
- retry backoff is capped;
- hub failure must not block collector loops.

### Linux hub

- SQLite is sufficient for the initial single-user/small-agent deployment;
- request handlers do not query vendor systems;
- snapshot reads are small and cheap;
- heavyweight brokers/databases are not introduced until measured need exists.

### Android

- one foreground Activity;
- no WebView/browser engine;
- no continuous animation;
- small JSON snapshots;
- minimal object churn;
- incremental UI updates.

Polling can later move to long polling or another event-friendly transport if measurements justify it.
