# AI Control HUD — Central Hub V2 Plan

Status: active development plan

## Objective

Move the always-on authority from the development machine to a 24/7 Linux server while preserving the existing local collectors and Android schema-v1 contract.

The target deployment is:

```text
Windows development machine              24/7 CentOS server                  Android HUD
┌──────────────────────────┐             ┌────────────────────────────┐       ┌──────────────────────┐
│ ZCode / CommandCode      │             │ AI Control Hub             │       │ Native Java HUD      │
│          │               │             │                            │       │                      │
│          ▼               │   outbound  │ latest canonical snapshot  │ HTTP  │ state dashboard      │
│ ai-control-agent (Go) ───┼────────────►│ agent heartbeat            ├──────►│ event cursor         │
│          │               │             │ durable event log          │       │ local TTS policy     │
│          ├ local API     │             │ SQLite                     │       │ quiet hours          │
│          └ upload/outbox │             │                            │       │                      │
└──────────────────────────┘             └────────────────────────────┘       └──────────────────────┘
```

## Architectural decisions

### 1. Keep local collection local

ZCode and CommandCode collection remains in `agent/`. The Linux server does not reach into the Windows machine and does not receive vendor credentials.

### 2. Preserve the local API

The Go agent continues to expose local `/api/v1/state` and `/api/v1/health` for diagnostics. The remote uploader consumes the same canonical `domain.HudState` held by `SnapshotStore`.

### 3. Windows pushes; the server never polls Windows

The development machine makes outbound requests to the hub. This avoids inbound Windows firewall/NAT requirements and makes a missing heartbeat an explicit machine-offline signal.

### 4. Snapshot and events are separate channels

A snapshot answers "what is true now?" and is replaceable.

An event answers "what happened?" and is durable and ordered. Task completion/failure notifications use the event stream rather than infer transitions only from Android polling.

### 5. CentOS is the Android authority

Android will eventually point to the Linux hub instead of directly to Windows. The public Android state payload remains schema-v1 while the hub migration is in progress.

### 6. Local notification policy stays on Android

The server records semantic events such as `task.completed`; Android decides whether to speak, vibrate, or remain silent. Quiet hours are local device configuration. Default quiet hours are 23:00–08:00.

### 7. Private networking first

The intended deployment is trusted LAN / Tailscale / WireGuard. Agent ingestion uses a bearer token. Direct public exposure is not part of this milestone.

## Hub protocol — phase 1

### Agent snapshot ingest

```http
POST /api/v1/agent/state
Authorization: Bearer <agent token>
Content-Type: application/json
```

```json
{
  "agentId": "desktop-main",
  "sentAt": "2026-09-16T11:00:00Z",
  "state": { "schemaVersion": 1 }
}
```

The complete `state` object is the existing schema-v1 canonical HUD state.

### Heartbeat

```http
POST /api/v1/agent/heartbeat
Authorization: Bearer <agent token>
```

```json
{
  "agentId": "desktop-main",
  "sentAt": "2026-09-16T11:00:10Z",
  "agentVersion": "0.3.0"
}
```

### Android compatibility API

```text
GET /api/v1/state
GET /api/v1/health
GET /api/v1/events?after=<seq>&limit=<1..100>
```

The hub serves the latest accepted canonical snapshot. If the agent heartbeat is older than the configured freshness threshold, previously trusted source data is retained but projected as `stale`; the aggregate state becomes `degraded`.

The event feed is independently schema-versioned. It exposes `nextAfter` for page consumption and `latestSeq` as the current high-water mark so a newly installed Android client can establish a silent baseline without replaying historical completions.

## Persistence

The Linux server uses SQLite.

Tables:

```text
agents
  agent_id PK
  agent_version
  last_seen_at

snapshots
  agent_id PK
  received_at
  state_json

events
  seq INTEGER PK AUTOINCREMENT
  event_id UNIQUE
  agent_id
  event_type
  occurred_at
  received_at
  event_json
```

No Redis, PostgreSQL, Kafka, MQTT, or Kubernetes is required for the first deployment.

## Delivery semantics — phase 2

Agent events use at-least-once delivery with idempotent server insertion:

1. transition detector creates an event with a stable `eventId`;
2. event is written to a local durable SQLite outbox;
3. uploader retries until the hub acknowledges it;
4. hub enforces `UNIQUE(event_id)`;
5. Android consumes events using monotonically increasing server `seq` cursors.

Task observation runs independently from event network delivery, so network backoff cannot stop local transition capture.

The first trustworthy snapshot seen by a new outbox establishes only a baseline. Historical completed/failed tasks are not replayed as new events.

## Android notification policy — phase 3

Initial policy:

- task completed: voice enabled by default outside quiet hours;
- task failed: voice enabled by default outside quiet hours;
- completed/failed voice preferences are independently configurable on-device;
- agent offline: visual only by default;
- quota warnings: visual only by default;
- events older than 10 minutes are not spoken individually;
- older catch-up pages produce one summary after the client reaches the event high-water mark;
- default quiet hours: 23:00–08:00, configurable on/off locally;
- TextToSpeech is performed locally on Android.

The dedicated-HUD foreground mode remains the primary operating mode. Background delivery mechanisms such as FCM are explicitly deferred until required by real device behavior.

## Iteration roadmap

### H0 — Architecture freeze

- [x] Record central-hub plan.
- [x] Update architecture/ADR docs after the first working vertical slice.

### H1 — Linux hub MVP

- [x] SQLite-backed hub store.
- [x] authenticated snapshot ingest.
- [x] authenticated heartbeat ingest.
- [x] schema-v1 `/api/v1/state` compatibility endpoint.
- [x] stale projection when an agent disappears.
- [x] API tests.

Exit criterion met: a schema-v1 snapshot can be authenticated, persisted, read back through the Android-compatible API, survive a hub restart, and be downgraded to stale after heartbeat expiry. Python CI passes on Linux and Windows.

### H2 — Go remote uploader

- [x] uploader package with bounded HTTP timeouts.
- [x] upload latest `SnapshotStore` state.
- [x] heartbeat cadence independent of collector cadence.
- [x] exponential retry/backoff.
- [x] configuration for hub URL, agent ID, token.
- [x] retain local HTTP API.

Exit criterion met at code/CI level: the production Go agent can refresh the hub using outbound-only requests while retaining local collection/API behavior; Go CI, native runtime smoke, and service smoke pass across the existing platform matrix. Real CentOS/Windows end-to-end validation belongs to H3/H6.

### H3 — Android cutover

- [ ] deploy the H1/H4 hub on the 24/7 CentOS host.
- [ ] configure the Windows agent to upload to that hub.
- [ ] point the existing HUD at the hub.
- [ ] show useful last-seen/offline state.
- [ ] validate Windows shutdown while Android remains usable.

### H4 — Durable event stream

- [x] define independently versioned event schema.
- [x] transition detector with safe first-run baseline.
- [x] durable local SQLite agent outbox.
- [x] independent observation and delivery loops.
- [x] idempotent hub event ingest by `eventId`.
- [x] monotonically ordered `seq` cursor API.
- [x] expose `latestSeq` high-water mark for first Android baseline.
- [x] persistence/retry/idempotency tests.

Exit criterion met at code/CI level: task terminal transitions survive agent restarts/network loss, may be delivered at least once without duplicate hub entries, survive hub restart, and can be consumed through a schema-v1 cursor feed. Python CI passes on Linux/Windows; Go vet/test/build and all existing service/SecretStore smoke checks pass across Windows/Linux/macOS.

### H5 — Android voice notifications

- [x] event client/parser with schema compatibility handling.
- [x] first-run event baseline from `latestSeq`.
- [x] persistent event cursor bound to the configured hub URL.
- [x] silent cursor rebase when the hub event database is reset.
- [x] TextToSpeech integration.
- [x] independent completed/failed voice preferences.
- [x] default 23:00–08:00 quiet hours with local enable/disable control.
- [x] 10-minute notification-age suppression plus one offline catch-up summary.
- [x] TTS package-visibility manifest declaration.
- [x] Android policy/cursor/time tests and CI build.

Exit criterion met at code/CI level: a new installation baselines silently, subsequent task events advance a persisted cursor, current completion/failure events can be spoken locally according to per-event preferences and quiet hours, old catch-up events are summarized instead of replayed individually, and event-feed failures do not mark the state dashboard offline. Android `lintDebug`, JVM unit tests, debug APK assembly, and artifact upload pass in CI. Real speaker/TTS-engine behavior remains a device-level H3/H6 validation item.

### H6 — Deployment hardening

- [ ] CentOS systemd unit.
- [ ] persistent data directory and backup notes.
- [ ] private-overlay deployment guide.
- [ ] protected Windows-service hub token storage.
- [ ] token rotation procedure.
- [ ] reboot/network-loss/outage tests.

## Non-goals for this migration

- controlling ZCode from the phone;
- exposing vendor credentials to the hub or Android;
- public Internet deployment by default;
- multi-user RBAC;
- heavyweight message brokers;
- exactly-once delivery;
- rewriting the existing collectors.

## Engineering rules

1. Existing schema-v1 Android compatibility must not silently break.
2. Source failures/offline states must never be represented as valid empty or zero data.
3. No token, API key, raw auth payload, or private log is stored in Git.
4. New network loops require bounded timeouts and cancellation.
5. Every iteration adds tests before the next protocol layer is introduced.
6. Local diagnostics remain usable even when the hub is unreachable.
