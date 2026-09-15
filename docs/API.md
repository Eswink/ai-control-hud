# Internal API Contract

Status: executable draft for Milestone 0. The shape is vendor-neutral and is validated by `server/hud/models.py` plus committed fixtures.

Base path:

```text
/api/v1
```

## Core availability rule

Source health and source data are separate concepts, but their combinations are constrained:

| health | data | meaning |
|---|---|---|
| `ok` | present | latest collection succeeded |
| `stale` | present | last-known-good data is retained, but the latest refresh failed or freshness policy was exceeded |
| `error` | `null` | no trustworthy data is currently available |
| `disabled` | `null` | source is intentionally disabled or not configured |

An adapter failure must never be represented as an empty list, zero counters, or zero balance. Those values could be real data and would be ambiguous.

All timestamps are ISO-8601 strings with an explicit timezone offset.

---

## `GET /api/v1/state`

Returns the latest normalized HUD snapshot from memory. The request handler must not directly invoke vendor APIs, CLIs, or read vendor databases.

### Healthy example

```json
{
  "schemaVersion": 1,
  "server": {
    "version": "0.1.0-dev",
    "time": "2026-09-15T21:30:00+08:00",
    "uptimeSeconds": 1234
  },
  "overall": {"status": "live"},
  "zcode": {
    "health": {
      "status": "ok",
      "observedAt": "2026-09-15T21:29:59+08:00",
      "lastSuccessAt": "2026-09-15T21:29:59+08:00",
      "message": null
    },
    "summary": {"running": 1, "waiting": 1, "failed": 0, "completed": 3},
    "tasks": [
      {
        "id": "opaque-task-id",
        "title": "Refactor agent pipeline",
        "workspace": "backend",
        "status": "running",
        "startedAt": "2026-09-15T21:10:00+08:00",
        "updatedAt": "2026-09-15T21:29:58+08:00",
        "durationSeconds": 1198,
        "activity": "pytest tests/api",
        "changes": {"additions": 428, "deletions": 103}
      }
    ]
  },
  "commandCode": {
    "health": {
      "status": "ok",
      "observedAt": "2026-09-15T21:29:30+08:00",
      "lastSuccessAt": "2026-09-15T21:29:30+08:00",
      "message": null
    },
    "usage": {
      "plan": "example-plan",
      "credit": {"remaining": 53.72, "limit": 70.0, "unit": "USD"},
      "windows": [
        {"name": "5h", "usedPercent": 81.0, "resetAt": "2026-09-15T23:41:00+08:00"},
        {"name": "weekly", "usedPercent": 63.0, "resetAt": "2026-09-19T06:00:00+08:00"}
      ]
    }
  }
}
```

All example values are synthetic.

### `schemaVersion`

Integer payload schema version. Android must reject unsupported future versions with a visible compatibility error rather than guessing.

### `overall.status`

- `live`: both required sources are `ok`.
- `degraded`: at least one required source is `stale`, `error`, or `disabled`.
- `offline` is client-derived when the backend itself cannot be reached; the server does not emit it in schema v1.

### Source health

`health.status` is one of `ok`, `stale`, `error`, `disabled`.

`observedAt` is when the source was last checked. `lastSuccessAt` is required for `ok` and `stale`; it may be `null` for `error`/`disabled`.

`message` is optional, short, and sanitized. It must not contain access tokens, cookies, complete private log lines, or secret file content.

### ZCode data

Canonical task status is one of:

- `running`
- `waiting`
- `failed`
- `completed`
- `unknown`

Vendor-specific values are mapped only inside the adapter.

When ZCode health is `error` or `disabled`, both `summary` and `tasks` are `null`. Optional fields inside a valid task may be `null` when the source cannot provide trustworthy values.

### CommandCode data

When CommandCode health is `error` or `disabled`, `usage` is `null`.

`credit.remaining`, `credit.limit`, window percentage, plan, and reset timestamp may individually be `null`/absent in future compatible expansions when the verified source cannot provide them. Do not fabricate values.

---

## `GET /api/v1/health`

Small backend reachability response used by Android setup and diagnostics.

```json
{
  "status": "ok",
  "schemaVersion": 1,
  "time": "2026-09-15T21:30:00+08:00",
  "sources": {
    "zcode": "ok",
    "commandCode": "stale"
  }
}
```

HTTP success means the HUD backend process is reachable, not that every data source is healthy.

---

## Error behavior

Unexpected backend errors use:

```json
{
  "error": {
    "code": "internal_error",
    "message": "sanitized diagnostic message"
  }
}
```

Normal vendor/source failures remain HTTP 200 at `/state` with the affected source represented through its health/data combination. This preserves partial availability.

---

## Android polling contract

Initial behavior:

- default polling interval: 2 seconds;
- one in-flight `/state` request at a time;
- bounded timeout and retry/backoff;
- no retry fan-out;
- reset countdowns are rendered locally from `resetAt` between successful snapshots;
- object field order is irrelevant;
- unknown optional fields must be ignored;
- unsupported `schemaVersion` must be shown as a compatibility error.

---

## Executable fixtures

The repository contains sanitized schema-v1 fixtures under `server/tests/fixtures/`:

- `healthy.json`
- `zcode_stale.json`
- `commandcode_auth_error.json`
- `backend_degraded.json`
- `task_failed.json`

They are the initial Android development contract and are validated by the Python model tests.

---

## Compatibility policy

`schemaVersion = 1` remains backward compatible after v1 release. Adding optional fields is allowed. Removing/renaming fields or changing field semantics requires a new schema version.
