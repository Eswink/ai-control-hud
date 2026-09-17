# Central Hub SQLite storage operations

Status: R2 low-I/O / long-running storage contract

The Hub keeps SQLite as the durable authority while minimizing repetitive read I/O for the mobile dashboard.

## State read cache

The Hub caches exactly one logical object in memory: the configured primary Agent's latest canonical state plus its last-seen timestamp.

- an accepted primary-Agent state POST is committed to SQLite first, then copied into the cache;
- an accepted primary-Agent heartbeat updates SQLite first, then advances the cached last-seen timestamp if the cache is already populated;
- after Hub restart, the first `/state` or `/health` read loads SQLite and seeds the cache;
- subsequent `/state` and `/health` polling reads a deep-cloned in-memory snapshot;
- non-primary Agent IDs are not added to a cache map, so authenticated arbitrary IDs cannot create an unbounded in-memory history.

SQLite remains the recovery authority. The cache is never written to disk separately.

## Storage statistics

Run locally on the CentOS host:

```bash
sudo /usr/local/lib/ai-control-hub/ai-control-hub stats \
  --database /var/lib/ai-control-hud/hub.sqlite3
```

Example shape:

```text
[hub-stats] database=/var/lib/ai-control-hud/hub.sqlite3 databaseBytes=12345678 walBytes=1048576
[hub-stats] events=14221 oldestSeq=120 latestSeq=14340
[hub-stats] pageSize=4096 pages=3100 freePages=175 reusableBytes=716800
[hub-stats] oldestReceivedAt=2026-06-19T10:00:00Z newestReceivedAt=2026-09-17T10:00:00Z
```

The command reports metadata only. It does not read/print Hub tokens, CommandCode credentials, Agent payload contents, or private task text.

`reusableBytes` is an estimate of pages SQLite has already freed internally (`freelist_count * page_size`). A database file that remains physically large after retention is therefore not automatically evidence of uncontrolled growth; SQLite can reuse those pages.

## WAL and lightweight maintenance

Every normal maintenance interval performs, after event retention:

```sql
PRAGMA optimize;
PRAGMA wal_checkpoint(PASSIVE);
```

`PASSIVE` checkpointing does not wait for active readers. The Hub still uses one `database/sql` connection for its primary store, WAL mode, and the existing busy timeout.

Do not add a large connection pool merely because Android polls frequently. The in-memory primary-state cache removes the repetitive snapshot JSON query/decode path while preserving the simpler serialized SQLite model.

## Full VACUUM

The Hub does **not** automatically run full `VACUUM`.

A full VACUUM rewrites the entire database, can temporarily require significant free disk space, and creates unnecessary I/O for a 24/7 service. Consider a manual shrink only after an unusual one-time database expansion and only during a maintenance window with a verified backup.

## What can still grow

- `agents` grows only with distinct accepted Agent IDs;
- `snapshots` has at most one latest row per Agent ID;
- `events` is bounded by the configured retention policy;
- the Agent-side pending outbox is a separate database and is not deleted by Hub retention.

Use `stats` periodically if you want to confirm that event count has reached a stable retention envelope. R0/R4 resource gates separately watch process-level RSS/FD/thread growth.
