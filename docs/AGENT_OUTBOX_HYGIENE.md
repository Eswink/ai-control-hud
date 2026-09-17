# Agent durable outbox hygiene

Status: R3 long-running storage policy

The development-machine Agent uses a local SQLite database for two durability jobs:

- `event_outbox`: terminal events accepted locally but not yet acknowledged by the Hub;
- `task_state`: the persistent task-status baseline used to avoid replaying historical completions/failures after restart.

## Non-negotiable durability rules

1. Pending `event_outbox` rows are never deleted merely to satisfy a disk-size target.
2. A Hub/network outage may grow the pending queue; the correct behavior is to warn and keep retrying.
3. Old terminal `task_state` rows cannot simply be deleted, because forgetting a task ID/status could cause the same historical terminal task to be emitted again if it reappears.
4. Automatic maintenance never runs full `VACUUM`.

## Automatic maintenance

The existing event-observation goroutine also performs low-frequency maintenance. No extra maintenance goroutine is created.

Defaults:

```text
maintenance interval: 6 hours
terminal baseline compact age: 180 days
maximum baseline rows compacted per pass: 1000
```

For a terminal (`completed`/`failed`) task not observed for at least 180 days, maintenance keeps:

```text
task_id
status
last_seen_at
```

and clears old display payload:

```text
title -> ""
workspace -> NULL
updated_at -> NULL
```

If the task later reappears, normal observation restores the payload while the retained identity/status prevents a duplicate semantic event.

Each pass also runs:

```sql
PRAGMA optimize;
PRAGMA wal_checkpoint(PASSIVE);   -- only when journal_mode is WAL
```

## Diagnostics

When Hub upload is configured, Agent-local:

```text
GET /api/v1/diagnostics
```

contains an optional sanitized `outbox` object:

```json
{
  "status": "ok",
  "pendingEvents": 3,
  "taskBaselineRows": 1250,
  "compactedTaskRows": 900,
  "oldestPendingAgeSeconds": 90,
  "reusableBytes": 32768
}
```

No database path, task content, event ID, event payload, Hub token, API key, or SecretStore data is returned.

Current warning policy:

- pending events >= 10,000; or
- oldest pending event age >= 24 hours.

A warning is informational/operational. It never triggers event deletion.

## Why row count may still grow

Compaction deliberately preserves task identity/status, so `task_state` row count can continue growing over years as unique task IDs accumulate. This is preferred to weakening event idempotency semantics. The payload footprint of old terminal rows is reduced substantially and SQLite can reuse free pages from other operations.

A future change may introduce a more compact tombstone representation only if tests prove it preserves the same no-replay guarantee. Do not delete old task baselines speculatively.

## Failure behavior

Maintenance errors are reported through the existing Agent error path and retried on a later maintenance interval. Collection, local state API, and pending-event durability remain independent from a maintenance failure.
