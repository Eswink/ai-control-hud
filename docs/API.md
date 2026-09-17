# Internal API Contract

Status: schema-v1 state contract is stable; Central Hub V2 adds a separately versioned schema-v1 event feed.

Base path:

```text
/api/v1
```

The Android-facing API is vendor-neutral. Request handlers must never directly query ZCode databases, provider APIs, or vendor credentials.

All timestamps are ISO-8601 strings with an explicit timezone offset.

---

# Android-facing state API

## Core availability rule

Source health and source data are separate concepts, but their combinations are constrained:

| health | data | meaning |
|---|---|---|
| `ok` | present | latest collection succeeded and the agent is fresh |
| `stale` | present | last-known-good data is retained, but collection failed, freshness expired, or the agent heartbeat expired |
| `error` | `null` | no trustworthy data is currently available |
| `disabled` | `null` | source is intentionally disabled or not configured |

A source/agent failure must never be represented as an empty list, zero counters, or zero balance. Those values could be real data and would be ambiguous.

## `GET /api/v1/state`

Returns the latest normalized HUD snapshot. During Central Hub V2 this is the latest accepted canonical snapshot for the configured primary agent, with hub-owned server time/uptime and connectivity freshness projected onto it.

### Healthy example

```json
{
  "schemaVersion": 1,
  "server": {
    "version": "0.2.0-hub-dev",
    "time": "2026-09-16T11:30:00Z",
    "uptimeSeconds": 1234
  },
  "overall": {"status": "live"},
  "zcode": {
    "health": {
      "status": "ok",
      "observedAt": "2026-09-16T11:29:59Z",
      "lastSuccessAt": "2026-09-16T11:29:59Z",
      "message": null
    },
    "summary": {"running": 1, "waiting": 1, "failed": 0, "completed": 3},
    "tasks": [
      {
        "id": "opaque-task-id",
        "title": "Refactor agent pipeline",
        "workspace": "backend",
        "status": "running",
        "startedAt": "2026-09-16T11:10:00Z",
        "updatedAt": "2026-09-16T11:29:58Z",
        "durationSeconds": 1198,
        "activity": "pytest tests/api",
        "changes": {"additions": 428, "deletions": 103}
      }
    ]
  },
  "commandCode": {
    "health": {
      "status": "ok",
      "observedAt": "2026-09-16T11:29:30Z",
      "lastSuccessAt": "2026-09-16T11:29:30Z",
      "message": null
    },
    "usage": {
      "plan": "example-plan",
      "credit": {"remaining": 53.72, "limit": 70.0, "unit": "USD"},
      "windows": [
        {"name": "5h", "usedPercent": 81.0, "resetAt": "2026-09-16T13:41:00Z"},
        {"name": "weekly", "usedPercent": 63.0, "resetAt": "2026-09-19T06:00:00Z"}
      ]
    }
  }
}
```

All example values are synthetic.

### State `schemaVersion`

Integer payload schema version. Android must reject unsupported future versions with a visible compatibility error rather than guessing.

### `overall.status`

- `live`: both required sources are `ok`.
- `degraded`: at least one required source is `stale`, `error`, or `disabled`.
- `offline` is client-derived when the hub itself cannot be reached; the server does not emit it in schema v1.

### Source health

`health.status` is one of `ok`, `stale`, `error`, `disabled`.

`observedAt` is when the source/health state was last evaluated. `lastSuccessAt` is required for `ok` and `stale`; it may be `null` for `error`/`disabled`.

`message` is optional, short, and sanitized. It must not contain access tokens, cookies, complete private log lines, or secret file content.

The hub may downgrade agent-reported `ok` data to `stale` after heartbeat expiry. It must not upgrade an agent-reported `stale`, `error`, or `disabled` source.

### ZCode data

Canonical task status is one of:

- `running`
- `waiting`
- `failed`
- `completed`
- `unknown`

Vendor-specific values are mapped only inside the development-machine collector.

When ZCode health is `error` or `disabled`, both `summary` and `tasks` are `null`. For trustworthy healthy/stale state, an empty task set is represented as `[]`, not `null`.

### CommandCode data

When CommandCode health is `error` or `disabled`, `usage` is `null`.

`credit.remaining`, `credit.limit`, window percentage, plan, and reset timestamp may individually be `null`/absent in future compatible expansions when the verified source cannot provide them. Do not fabricate values.

---

## `GET /api/v1/health`

Small hub reachability response used by Android setup and diagnostics.

```json
{
  "status": "ok",
  "schemaVersion": 1,
  "time": "2026-09-16T11:30:00Z",
  "sources": {
    "zcode": "ok",
    "commandCode": "stale"
  }
}
```

HTTP success means the HUD/hub process is reachable, not that every source or development machine is healthy.

---

# Android-facing durable event API

Snapshot state answers **what is true now**. The event feed answers **what happened** and is the authoritative input for user-facing completion/failure notifications.

Android must not infer durable notifications only by comparing successive `/state` snapshots.

## `GET /api/v1/events`

Query parameters:

- `after`: non-negative server sequence cursor; default `0`;
- `limit`: page size `1..100`; default `100`.

Example:

```text
GET /api/v1/events?after=172&limit=100
```

Response:

```json
{
  "schemaVersion": 1,
  "events": [
    {
      "seq": 173,
      "eventId": "0123456789abcdef0123456789abcdef",
      "agentId": "desktop-main",
      "type": "task.completed",
      "occurredAt": "2026-09-16T11:31:02Z",
      "receivedAt": "2026-09-16T11:31:03Z",
      "task": {
        "id": "opaque-task-id",
        "title": "Refactor agent pipeline",
        "workspace": "backend",
        "status": "completed"
      }
    }
  ],
  "nextAfter": 173,
  "latestSeq": 173
}
```

### Event feed `schemaVersion`

The event feed has its own `schemaVersion`. Schema v1 currently supports:

- `task.completed`
- `task.failed`

Android must reject unsupported event-feed schema versions rather than guessing their semantics.

### `seq`

`seq` is a monotonically increasing hub-assigned cursor. It is not an event identity and it is not required to be gap-free for a particular agent.

Clients persist the last successfully consumed sequence and request `after=<cursor>` on the next fetch.

### `eventId`

`eventId` is the stable event identity generated by the development-machine agent and used by the hub for idempotent insertion. Retries of the same `eventId` do not create duplicate feed entries.

### `nextAfter`

`nextAfter` is the cursor after consuming every event returned in this page. When `events` is empty it equals the requested `after` value.

After successfully processing a page, Android persists `nextAfter` even when notification policy decides not to speak an event.

### `latestSeq`

`latestSeq` is the current high-water mark for the primary agent's event feed at the time the page is read. It may be greater than `nextAfter` when additional pages remain.

On the **first Android connection only**, when no event cursor has ever been persisted, the client sets its cursor directly to `latestSeq` without speaking returned historical events. This creates a clean baseline and prevents installing/reconfiguring the app from replaying old task completions.

After a cursor exists, Android must continue from that persisted cursor rather than resetting to `latestSeq`; this is what permits bounded offline/reconnect catch-up.

### Notification policy is not part of the server contract

The event API reports semantic facts. It does not specify speech, volume, vibration, quiet hours, or whether an old event should interrupt the user.

Android owns those policies locally. Advancing/persisting the cursor is independent from whether an event is spoken.

---

# Development-machine agent ingest API

These endpoints are Hub-internal transport contracts, not Android APIs.

All requests require:

```text
Authorization: Bearer <agent token>
Content-Type: application/json
```

The agent token must not be stored in the APK, URL query string, Git repository, or log output.

## `POST /api/v1/agent/state`

Uploads the latest canonical schema-v1 snapshot:

```json
{
  "agentId": "desktop-main",
  "sentAt": "2026-09-16T11:30:00Z",
  "state": {
    "schemaVersion": 1
  }
}
```

The real `state` value is the complete canonical `HudState`; it is abbreviated above only for documentation readability.

## `POST /api/v1/agent/heartbeat`

```json
{
  "agentId": "desktop-main",
  "sentAt": "2026-09-16T11:30:10Z",
  "agentVersion": "0.3.0-go-dev"
}
```

The hub uses its own receive timestamp as the connectivity freshness authority so development-machine clock drift cannot make an offline machine appear fresh.

## `POST /api/v1/agent/events`

Uploads `1..100` durable events in one request:

```json
{
  "agentId": "desktop-main",
  "sentAt": "2026-09-16T11:31:03Z",
  "events": [
    {
      "eventId": "0123456789abcdef0123456789abcdef",
      "type": "task.completed",
      "occurredAt": "2026-09-16T11:31:02Z",
      "task": {
        "id": "opaque-task-id",
        "title": "Refactor agent pipeline",
        "workspace": "backend",
        "status": "completed"
      }
    }
  ]
}
```

Response:

```json
{
  "status": "accepted",
  "agentId": "desktop-main",
  "receivedAt": "2026-09-16T11:31:03Z",
  "accepted": 1,
  "duplicates": 0
}
```

### Event delivery semantics

Agent event transport is **at least once**:

1. the agent observes a trustworthy task transition;
2. it first writes the event to its local SQLite outbox;
3. upload retries continue until an HTTP success response is received;
4. the hub inserts events with `UNIQUE(event_id)` semantics;
5. only after successful upload does the agent remove those event IDs from the local outbox.

If the hub accepted a request but the response/agent acknowledgement path fails, the same event may be uploaded again. The hub treats that retry as a duplicate rather than creating another Android event.

The agent's event-observation loop is independent from the network-delivery loop. Network backoff therefore does not stop local task-transition capture.

### Startup baseline

The first trustworthy ZCode snapshot observed by a newly created local outbox establishes a task baseline only. Existing `completed`/`failed` tasks are not emitted as new events.

After baseline creation:

- a transition into `completed` or `failed` emits one event;
- a previously unseen task that first appears already terminal also emits one event, covering very fast tasks;
- repeated observation of the same terminal status does not emit another event;
- stale/error ZCode snapshots do not advance the event baseline.

---

# Error behavior

Unexpected backend errors use a sanitized error response. Normal vendor/source failures remain represented through source health/data rather than being converted to fake valid values.

Agent ingest authentication failure returns HTTP 401. Validation failure returns an HTTP 4xx response and must not be treated as a successful outbox acknowledgement.

---

# Android polling / consumption contract

State behavior:

- one in-flight state request at a time;
- bounded timeout and retry/backoff;
- reset countdowns are rendered locally from `resetAt` between successful snapshots;
- object field order is irrelevant;
- unknown optional fields must be ignored;
- unsupported state `schemaVersion` must be shown as a compatibility error.

Event behavior for H5:

- persist the consumed event cursor in `SharedPreferences`;
- first-run initialization baselines to `latestSeq` without speech;
- subsequent polling requests `after=<persisted cursor>`;
- process events in ascending `seq` order;
- persist `nextAfter` after successful page processing even when an event is silent because of quiet hours/age/user settings;
- old/offline events may be suppressed from speech without being replayed later;
- event-feed schema incompatibility is explicit and must not silently reset the cursor.

---

# Executable fixtures

The repository contains sanitized schema-v1 state fixtures under `server/tests/fixtures/`:

- `healthy.json`
- `zcode_stale.json`
- `commandcode_auth_error.json`
- `backend_degraded.json`
- `task_failed.json`

Hub event behavior is covered by executable API tests, including authentication, idempotency, pagination/cursors, high-water marks, primary-agent filtering, validation, and persistence across hub restart.

---

# Compatibility policy

State `schemaVersion = 1` remains backward compatible after v1 release. Adding optional fields is allowed. Removing/renaming fields or changing field semantics requires a new state schema version.

Event-feed `schemaVersion = 1` follows the same rule independently. New optional fields may be added compatibly; changing cursor semantics, event identity semantics, or existing event-type meaning requires a new event-feed schema version.
