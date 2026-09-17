# AI Control HUD resource and long-running reliability budgets

Status: R0-R4 reliability contract

These values are engineering targets, not protocol fields. Absolute memory numbers remain **soft baselines** until several release-build measurements establish runner/device variance; monotonic resource growth and lifecycle leaks are hard failures now.

## Production principles

- The Windows Agent service, Central Hub, Android UI, and future Windows UI must remain usable for continuous multi-day operation.
- A UI process may exit without affecting the Agent service.
- No component may rely on an unbounded in-memory event/history cache.
- Pending Agent events are durable and must never be deleted merely to satisfy a size target.
- Hub event retention is explicit and bounded; snapshot state remains latest-only.
- No public pprof/debug endpoint is enabled in production.
- Maintenance workers must be context-driven and settle after cancellation.

## Initial soft steady-state targets

| Component | Initial target | Notes |
| --- | ---: | --- |
| Windows native Agent UI | <= 48 MiB working set | future C++/Win32/Direct2D target; hidden UI should be near-idle CPU |
| Windows Go Agent service | <= 96 MiB working set | collector/uploader/outbox included |
| CentOS Go Hub | <= 96 MiB RSS | one SQLite connection, WAL, no GUI |
| Android HUD | <= 96 MiB PSS | old-phone target, Java/XML Views, no WebView/Compose |

Absolute values become hard gates only after several stable release-build measurements establish runner/device variance.

## Measured Hub baselines

### R0 lightweight read baseline

Go Hub CI #60 measured the statically linked PR #93 release binary on the GitHub-hosted Ubuntu 24.04 runner with Go 1.27.1. After readiness, the smoke issued 1,000 loopback `GET /api/v1/health` requests and sampled Linux `/proc` before and after the workload:

| Metric | Before | After | Growth |
| --- | ---: | ---: | ---: |
| RSS | 13,980 KiB | 18,892 KiB | +4,912 KiB (~4.8 MiB) |
| file descriptors | 10 | 10 | 0 |
| OS threads | 8 | 9 | +1 |

### R4 mixed workload baseline

Go Hub CI #82 measured the PR #96 release binary with 2,000 durable events, 1,000 state/health reads, 500 authenticated heartbeats, periodic state uploads, event pagination, a clean restart, and retention convergence:

| Metric | Before | After | Growth |
| --- | ---: | ---: | ---: |
| RSS | 16,492 KiB | 22,068 KiB | +5,576 KiB (~5.4 MiB) |
| file descriptors | 10 | 10 | 0 |
| OS threads | 8 | 9 | +1 |

After restart the synthetic `max=1000` retention policy converged to `oldestSeq=1001`, `latestSeq=2000`, proving the hard event cap through the real binary lifecycle.

These are hosted-runner reference points, not absolute production memory promises.

## First measured Windows Agent baseline

Go Agent CI #396 measured the Windows release/debug-CI binary on the GitHub-hosted Windows Server 2025 runner after API warm-up and 1,500 local requests cycling `/state`, `/health`, and `/diagnostics`:

| Metric | Before | After | Growth |
| --- | ---: | ---: | ---: |
| Working Set | 11.0 MiB | 15.4 MiB | +4.4 MiB |
| Private Bytes | 47.1 MiB | 51.1 MiB | +4.0 MiB |
| handles | 144 | 152 | +8 |
| OS threads | 9 | 10 | +1 |
| CPU during 2s idle sample | — | 0.000 s | effectively idle |

The handle delta is exactly at the current +8 gross guard. It is therefore a value to watch across future runs before tightening the guard; it is not evidence by itself of a leak because the workload remains bounded and the job completed successfully.

## R4 hard growth guards

### CentOS Go Hub

Two release-binary gates complement Go unit lifecycle tests.

**Lightweight read gate**

- 1,000 loopback `/api/v1/health` requests;
- reject > +4 file descriptors;
- reject > +4 OS threads;
- reject > +64 MiB RSS.

**Mixed workload soak**

The release Hub binary receives an accelerated workload containing:

- repeated authenticated heartbeats;
- repeated canonical state uploads;
- thousands of durable event inserts in bounded batches;
- repeated `/state` and `/health` reads through the primary-state cache;
- event cursor pagination;
- clean process restart;
- immediate retention convergence to a small synthetic hard cap.

The same gross RSS/FD/thread growth guards apply. The soak also proves retention works after a real binary restart, not only in package unit tests.

**Go lifecycle gate**

Repeated maintenance worker start/cancel cycles must settle within +4 goroutines of the warmed baseline.

### Windows Go Agent service/binary

The Windows release binary is started with the shared canonical fixture and exercised through real local HTTP requests.

After a warm-up phase CI records:

- Working Set;
- Private Bytes;
- process handle count;
- OS thread count;
- process CPU time.

Current gross PR guards:

- +8 handles maximum;
- +4 threads maximum;
- +64 MiB Working Set maximum growth;
- +64 MiB Private Bytes maximum growth;
- <= 0.20 seconds process CPU consumed during a two-second idle sample.

The workload cycles `/api/v1/state`, `/api/v1/health`, and `/api/v1/diagnostics` 1,500 times. These are leak guards, not the final <=96 MiB production promise.

**Go lifecycle gate**

Repeated remote runtime start/cancel cycles exercise snapshot, heartbeat, event-observation, event-delivery, outbox maintenance, and durable outbox close. Goroutine count must settle within +4 of the warmed baseline.

### Windows native UI

When implemented, native test loops will repeatedly open/close windows and settings surfaces and track:

- working set / private bytes;
- process handle count;
- USER objects;
- GDI objects;
- WinHTTP handles;
- COM/Direct2D resource ownership.

All owning native resources must use deterministic RAII cleanup. UI4 cannot be considered release-ready until this harness exists.

### Android

Orientation, Activity recreation, polling, event refresh, and TTS initialization/shutdown must not retain destroyed Activities or obsolete network callbacks. Android lifecycle/PSS soak is intentionally implemented with UI1/UI3, when the real portrait/landscape lifecycle exists, instead of testing the current portrait-only layout as a false proxy.

## Hub storage budget

Hub schema behavior after R1/R2:

- `agents`: bounded by Agent IDs and updated in place;
- `snapshots`: one latest row per Agent, updated in place;
- `events`: durable history bounded by retention;
- primary Android-facing state/health reads: bounded in-memory latest-state cache after first seed/read.

R1 default event retention policy:

```text
HUD_HUB_EVENT_RETENTION_DAYS=90
HUD_HUB_EVENT_RETENTION_MIN=10000
HUD_HUB_EVENT_RETENTION_MAX=100000
HUD_HUB_MAINTENANCE_INTERVAL_HOURS=6
```

Age pruning preserves at least the newest minimum rows per Agent. The maximum is a hard safety cap. Deletes are performed in batches of 1,000 rows using separate transactions.

Retention applies only to events already accepted by the Hub. It never deletes the Windows Agent's pending durable outbox.

## Agent local durable storage policy

R3 keeps at-least-once semantics ahead of disk cosmetics:

- pending `event_outbox` rows are never automatically discarded;
- >=10,000 pending events or an oldest pending age >=24h produces a diagnostics warning instead of deletion;
- terminal `task_state` rows inactive for >=180 days keep task identity/status/last-seen but clear title/workspace/updated-at payload;
- at most 1,000 baseline rows are compacted per maintenance pass;
- maintenance reuses the existing observation goroutine and does not create another permanent worker.

## SQLite strategy

Hub and Agent durable databases intentionally keep a small serialized architecture:

- one `database/sql` connection per DB;
- WAL mode for persistent databases;
- `busy_timeout=5000`;
- short transactions;
- relevant cursor/event indexes.

Do not add a connection pool without benchmark evidence. The deployment is low-QPS and benefits from simple ownership.

R2 added:

- bounded primary-state/last-seen cache for `/state` and `/health`;
- `ai-control-hub stats` local CLI;
- `PRAGMA optimize` + passive WAL checkpoint maintenance;
- DB/WAL/event/page/freelist observability.

Automatic frequent full `VACUUM` remains explicitly out of scope because it rewrites the entire database and can create avoidable I/O/latency spikes. Free pages are reusable even when the file does not immediately shrink.

## Retention cursor semantics

The event response keeps `schemaVersion=1` and adds the optional-compatible field:

```json
{
  "oldestSeq": 12345
}
```

`oldestSeq=0` means no events are currently retained. A new Android client can detect `savedCursor < oldestSeq - 1`, inform the user once that part of history expired, and rebase without fabricating missing events. Existing Android clients ignore the extra JSON field.

## Interpreting CI resource numbers

Hosted runners are noisy. A one-off higher RSS value is not by itself a leak. The hierarchy is:

1. FD/handle/thread/goroutine monotonic growth is a hard regression;
2. very large memory growth in a bounded workload is a hard regression;
3. absolute steady-state RSS/Working Set/PSS is recorded and compared across several release builds;
4. after variance is understood, the soft <=96/48 MiB targets may be promoted to hard budgets.

Do not lower resource use by silently dropping durable data or disabling safety checks.
