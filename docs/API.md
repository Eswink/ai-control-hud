# Internal API Contract

Status: draft for Milestone 0. The shape is intentionally vendor-neutral.

Base path:

```text
/api/v1
```

---

## `GET /api/v1/state`

Returns the latest normalized HUD snapshot from memory. The handler must not directly invoke vendor APIs or read vendor databases.

### Example

```json
{
  "schemaVersion": 1,
  "server": {
    "version": "0.1.0-dev",
    "time": "2026-09-15T21:30:00+08:00",
    "uptimeSeconds": 1234
  },
  "overall": {
    "status": "live"
  },
  "zcode": {
    "health": {
      "status": "ok",
      "observedAt": "2026-09-15T21:29:59+08:00",
      "lastSuccessAt": "2026-09-15T21:29:59+08:00",
      "message": null
    },
    "summary": {
      "running": 1,
      "waiting": 1,
      "failed": 0,
      "completed": 3
    },
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
        "changes": {
          "additions": 428,
          "deletions": 103
        }
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
      "credit": {
        "remaining": 53.72,
        "limit": 70.0,
        "unit": "USD"
      },
      "windows": [
        {
          "name": "5h",
          "usedPercent": 81.0,
          "resetAt": "2026-09-15T23:41:00+08:00"
        },
        {
          "name": "weekly",
          "usedPercent": 63.0,
          "resetAt": "2026-09-19T06:00:00+08:00"
        }
      ]
    }
  }
}
```

All example values are synthetic.

### Field rules

#### `schemaVersion`

Integer API payload schema version. Android must reject unsupported future versions with a visible compatibility error rather than guessing.

#### `overall.status`

One of:

- `live`
- `degraded`
- `offline` (primarily client-derived when the backend cannot be reached)

The server generally emits `live` or `degraded`; Android derives backend `offline` after request failure/timeouts.

#### Source health

`health.status` is one of:

- `ok`
- `stale`
- `error`
- `disabled`

`message` must be sanitized and must not contain access tokens, cookies, full private log lines, or sensitive filesystem contents.

#### Task status

Canonical task status is one of:

- `running`
- `waiting`
- `failed`
- `completed`
- `unknown`

Vendor-specific values are mapped inside the adapter.

Optional fields may be `null` when a source does not provide trustworthy data. Do not fabricate zero values.

---

## `GET /api/v1/health`

Small backend liveness/readiness response intended for setup testing and diagnostics.

### Example

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

HTTP success indicates that the HUD backend process is reachable, not that every source is healthy. Source status must be inspected separately.

---

# Error behavior

## Backend API errors

Unexpected backend errors use JSON:

```json
{
  "error": {
    "code": "internal_error",
    "message": "sanitized diagnostic message"
  }
}
```

## Adapter errors

Normal vendor/source failures should usually remain HTTP 200 at `/state` with the affected source health set to `stale` or `error`. This preserves the other source's data and makes partial failure explicit.

---

# Client polling contract

Initial Android behavior:

- Default polling interval: 2 seconds.
- Connection timeout: conservative and shorter than the poll interval where practical.
- Only one in-flight `/state` request at a time.
- Failed requests use bounded backoff rather than spawning concurrent retries.
- Countdown display is derived locally from `resetAt` between successful snapshots.

The exact retry/backoff policy will be finalized during device testing.

---

# Compatibility policy

`schemaVersion = 1` remains backward compatible after v1 release. Adding optional fields is allowed. Removing/renaming fields or changing semantics requires a new schema version.

Android must not depend on JSON object field ordering.
