# Central Hub event retention

Status: R1 long-running storage policy

The Hub stores only the latest state snapshot per Agent, but durable task events are historical rows. R1 bounds that historical growth without touching pending Agent-side outbox data.

## Defaults

```text
HUD_HUB_EVENT_RETENTION_DAYS=90
HUD_HUB_EVENT_RETENTION_MIN=10000
HUD_HUB_EVENT_RETENTION_MAX=100000
HUD_HUB_MAINTENANCE_INTERVAL_HOURS=6
```

The production defaults are applied automatically when these variables are absent.

### Meaning

For each Agent ID:

- events older than 90 days become eligible for age pruning;
- the newest 10,000 events are protected from age pruning;
- 100,000 retained events is a hard safety maximum even when they are younger than 90 days;
- delete work is committed in batches of at most 1,000 rows.

The maximum must be greater than or equal to the minimum.

## Why retention is safe for snapshots

`agents` and `snapshots` do not contain an ever-growing state history. The snapshot table has one row per Agent ID and is updated in place. Retention operates only on `events`.

## Agent outbox is independent

The Windows/Linux/macOS Agent has a separate durable SQLite event outbox. Hub retention does not access or delete it. Events waiting to reach the Hub therefore remain subject to the existing at-least-once delivery rules.

## Event cursor floor

`GET /api/v1/events` remains event schema v1 and now includes:

```json
{
  "schemaVersion": 1,
  "events": [],
  "nextAfter": 0,
  "latestSeq": 0,
  "oldestSeq": 0
}
```

`oldestSeq` is the oldest event sequence currently retained for the configured primary Agent. It is `0` when no events are retained.

The field is additive and existing Android clients ignore it. New clients can detect:

```text
savedCursor < oldestSeq - 1
```

as an explicit retention gap. A client must not fabricate the missing events. It may notify the user once and safely rebase to the retained range.

## Maintenance lifecycle

The Go Hub starts one maintenance goroutine with the Hub process context:

1. run one retention pass asynchronously at startup;
2. sleep for the configured interval;
3. repeat until Hub shutdown;
4. stop when the Hub context is cancelled.

Each delete batch is a separate transaction. This avoids one large long-lived DELETE transaction and allows normal request work to progress between batches.

## SQLite and file size

Deleting rows does not necessarily make `hub.sqlite3` immediately smaller. SQLite adds freed pages to its freelist and normally reuses them for future rows. This is expected and is preferable to frequent full-file rewrites.

Do **not** schedule frequent automatic `VACUUM`. A full VACUUM rewrites the database, can temporarily require substantial extra disk space, and creates unnecessary I/O/latency spikes.

R2 will add storage statistics and bounded WAL/`PRAGMA optimize` maintenance so operators can distinguish reusable free pages from real uncontrolled growth.

## Tuning examples

Keep one year of history with the same hard maximum:

```text
HUD_HUB_EVENT_RETENTION_DAYS=365
HUD_HUB_EVENT_RETENTION_MIN=10000
HUD_HUB_EVENT_RETENTION_MAX=100000
```

Favor a smaller database while still retaining a useful event tail:

```text
HUD_HUB_EVENT_RETENTION_DAYS=30
HUD_HUB_EVENT_RETENTION_MIN=2000
HUD_HUB_EVENT_RETENTION_MAX=20000
```

After changing the root-owned Hub environment, restart the service so the new policy is loaded. Never place bearer tokens or other credentials in shell history while editing unrelated retention settings.
