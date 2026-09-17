# AI Control HUD resource and long-running reliability budgets

Status: R0 baseline contract

These values are engineering targets, not protocol fields. The first CI iteration treats absolute memory numbers as **soft baselines** because hosted-runner measurements vary; monotonic resource growth remains a hard failure.

## Production principles

- The Windows Agent service, Central Hub, Android UI, and future Windows UI must remain usable for continuous multi-day operation.
- A UI process may exit without affecting the Agent service.
- No component may rely on an unbounded in-memory event/history cache.
- Pending Agent events are durable and must never be deleted merely to satisfy a size target.
- Hub event retention is explicit and bounded; snapshot state remains latest-only.
- No public pprof/debug endpoint is enabled in production.

## Initial soft steady-state targets

| Component | Initial target | Notes |
| --- | ---: | --- |
| Windows native Agent UI | <= 48 MiB working set | C++/Win32/Direct2D target; hidden UI should be near-idle CPU |
| Windows Go Agent service | <= 96 MiB working set | collector/uploader/outbox included |
| CentOS Go Hub | <= 96 MiB RSS | one SQLite connection, WAL, no GUI |
| Android HUD | <= 96 MiB PSS | old-phone target, Java/XML Views, no WebView/Compose |

Absolute values become hard gates only after several stable release-build measurements establish runner/device variance.

## Hard growth guards

### Hub / Go services

Repeated bounded workloads must not cause linear growth in:

- RSS after a post-workload settling period;
- open file descriptors;
- OS threads;
- Go goroutines;
- SQLite connection count;
- retained in-memory history.

R0 Linux CI currently performs 1,000 loopback `/api/v1/health` requests against the release Hub binary and rejects gross growth of more than:

- +4 file descriptors;
- +4 OS threads;
- +64 MiB RSS.

The RSS limit is intentionally a **gross leak guard**, not the final Hub budget.

### Windows native UI

When implemented, automated/native test loops should repeatedly open/close windows and settings surfaces and track:

- working set / private bytes;
- process handle count;
- USER objects;
- GDI objects;
- WinHTTP handles;
- COM/Direct2D resource ownership.

All owning native resources must use deterministic RAII cleanup.

### Android

Orientation, Activity recreation, polling, event refresh, and TTS initialization/shutdown must not retain destroyed Activities or obsolete network callbacks. Tests/soaks should monitor PSS and, where practical, heap/object counts across repeated portrait/landscape recreation.

## Hub storage budget

Current Hub schema behavior:

- `agents`: bounded by Agent IDs and updated in place;
- `snapshots`: one latest row per Agent, updated in place;
- `events`: append-only history and therefore the primary long-term growth source.

R1 default event retention policy:

```text
HUD_HUB_EVENT_RETENTION_DAYS=90
HUD_HUB_EVENT_RETENTION_MIN=10000
HUD_HUB_EVENT_RETENTION_MAX=100000
HUD_HUB_MAINTENANCE_INTERVAL_HOURS=6
```

Age pruning preserves at least the newest minimum rows per Agent. The maximum is a hard safety cap. Deletes are performed in batches of 1,000 rows using separate transactions.

Retention applies only to events already accepted by the Hub. It never deletes the Windows Agent's pending durable outbox.

## SQLite strategy

The Hub intentionally keeps:

- one `database/sql` connection;
- WAL mode;
- `busy_timeout=5000`;
- the `(agent_id, seq)` event index.

Do not add a connection pool without benchmark evidence. The deployment is low-QPS and benefits from simple serialized SQLite ownership.

R2 will add:

- latest-state/health read cache to eliminate repetitive state JSON reads for Android polling;
- storage diagnostics (`ai-control-hub stats`);
- periodic `PRAGMA optimize` and bounded WAL checkpoint policy;
- DB/WAL/event-count observability.

Automatic frequent full `VACUUM` is explicitly out of scope because it rewrites the entire database and can create avoidable I/O/latency spikes.

## Retention cursor semantics

The event response keeps `schemaVersion=1` and adds the optional-compatible field:

```json
{
  "oldestSeq": 12345
}
```

`oldestSeq=0` means no events are currently retained. A future/new Android client can detect `savedCursor < oldestSeq - 1`, inform the user once that part of history expired, and rebase without fabricating missing events. Existing Android clients ignore the extra JSON field.
