# AI Control HUD — Agent UI & Long-Running Reliability Plan v2

Status: revised architecture / implementation planning  
Scope: Windows Agent UI + Android Agent-monitor UI + Agent/Hub long-running resource and storage hardening. Hub administration UI remains out of scope.

## 0. Revision summary

V2 changes the implementation priority after the operator established three additional requirements:

1. Windows UI must be as native as practical and avoid WebView/Electron-class memory overhead.
2. Windows, Android, Agent service, and CentOS Hub must be designed for low idle CPU and low steady memory.
3. Memory/resource leaks must be treated as release-blocking regressions, especially for Windows Agent/UI and the always-on Hub.
4. Hub SQLite data growth must be bounded with an explicit retention and maintenance policy.

The three approved reference images remain the visual target, but they no longer determine the technology stack.

---

## 1. Product architecture stays separated

```text
Windows interactive session
  ai-control-agent-ui.exe       native desktop UI
          |
          | local read-only HTTP + explicit elevated CLI actions
          v
Windows Session 0
  ai-control-agent.exe          Go service / collectors / uploader
          |
          | outbound state + heartbeat + durable events
          v
CentOS
  ai-control-hub                Go daemon + SQLite
          |
          | schema-v1 state/events
          v
Android
  native Java/XML HUD
```

Rules:

- UI failure must never stop collection/upload.
- Hub remains headless; no Hub administration GUI is added.
- Closing Windows UI does not stop the Agent service.
- Android remains a viewer/notification client, not a credential holder.
- Credentials/private paths never appear in UI or telemetry.

---

## 2. Resource philosophy

### 2.1 No permanent animation loop

No surface is allowed to run a 30/60 FPS render loop while data is unchanged.

- Windows repaints only on `WM_PAINT`, data change, resize, hover/focus, or a short transition.
- Android uses normal View invalidation only when state changes.
- Progress rings are static drawings, not continuously rotating indicators.
- Success/failure transitions may animate briefly (target <= 1.5 s), then stop.

### 2.2 Adaptive polling

Polling is based on visibility, not process lifetime.

Suggested initial policy:

| Client state | State refresh | Diagnostics/event refresh |
|---|---:|---:|
| Android foreground portrait | 2 s | event feed 2–5 s |
| Android foreground landscape focus | 2 s | event/TTS secondary 5 s |
| Android background | stopped | stopped |
| Windows UI visible | 2 s | diagnostics 5 s |
| Windows UI minimized/hidden to tray | 15–30 s health only | detail polling stopped |
| Windows UI exited | none | none |

The Agent service and Hub continue independently.

### 2.3 Release-build budgets

Absolute memory varies by OS, but we define soft budgets plus regression limits. First implementation records baselines; later CI enforces the measured values.

Initial targets (release builds, after warm-up):

| Process | Soft steady-memory target | Idle / steady CPU target |
|---|---:|---:|
| Windows native UI | <= 48 MiB working set | ~0% hidden, <1% visible average |
| Windows Go Agent | <= 96 MiB working set | <1% when collectors are idle/steady |
| CentOS Go Hub | <= 96 MiB RSS | <1% at normal one-agent/one-phone load |
| Android portrait | <= 96 MiB PSS | no continuous animation; low single-digit CPU |
| Android landscape focus | <= portrait PSS | no continuous animation; low single-digit CPU |

A build is considered suspicious even below the absolute limit if, after warm-up, memory/handle/goroutine count shows a sustained upward trend during a soak test.

---

## 3. Windows GUI — revised native-first architecture

### 3.1 Technology decision

**Do not use Wails, Electron, WebView2, Qt WebEngine, or another embedded browser for the production Windows UI.**

Primary implementation target:

- C++20
- classic Win32 window/message loop
- Direct2D for cards/progress/status graphics
- DirectWrite for text
- Windows Imaging Component only if an image asset is unavoidable
- WinHTTP for localhost HTTP
- native NotifyIcon/system tray
- ShellExecute/UAC for privileged service actions
- system Segoe UI / Microsoft YaHei UI fonts; no bundled font runtime

Why:

- no browser runtime;
- no .NET/Windows App SDK requirement for the main UI path;
- event-driven idle behavior;
- small working set;
- direct control over rendering and lifetime;
- native tray/UAC/window behavior.

WinUI 3/C++ may be reconsidered only if a measured prototype stays inside the same memory/CPU envelope. It is not the default because Windows App SDK/XAML adds runtime overhead.

### 3.2 Memory ownership rules

Native UI code must follow strict ownership rules:

- no owning raw pointers;
- no application-level `new/delete` pairs when RAII containers/smart pointers can own the object;
- COM objects held with `Microsoft::WRL::ComPtr` or an equivalent RAII wrapper;
- Win32 `HANDLE`, WinHTTP handles, registry keys, icons, brushes, timers and wait handles wrapped in deterministic destructors;
- one explicit owner for every HWND child/control model;
- no static object may retain an HWND, callback, network request, or view-model after window destruction;
- timers are cancelled before window/model teardown;
- worker results carry a generation/cancellation token so a closed window cannot receive stale callbacks;
- Direct2D device-dependent resources are released on device loss and window destruction.

### 3.3 Rendering strategy

- One UI thread.
- Network/JSON work on a small bounded worker (normally one worker thread).
- UI receives immutable view-state snapshots.
- Compare new and old view-state; invalidate only changed cards when practical.
- No timer exists only for visual animation.
- No large bitmap background; cards/gradients are drawn procedurally.
- Reuse text formats, brushes and geometry; do not recreate them each frame.

### 3.4 Windows leak/regression gates

CI / test harness should cover:

- create/destroy the main window repeatedly (target 200–500 cycles);
- open/close each detail/settings dialog repeatedly;
- repeated state refreshes (10k+ synthetic updates);
- service-action cancellation/error paths;
- WinHTTP request timeout/cancel paths;
- working-set/private-bytes trend after warm-up;
- process handle count;
- USER/GDI object count (`GetGuiResources`);
- optional Application Verifier / page heap job for nightly CI;
- MSVC AddressSanitizer for testable native modules where supported;
- `/analyze` / static analysis warnings treated seriously.

The Windows UI is restartable and disposable by design; no data durability depends on it.

---

## 4. Windows Agent service — long-running resource rules

The Go Agent remains the collector/service.

### 4.1 Leak prevention

Every long-lived component must have a bounded lifecycle:

- goroutines owned by a `context.Context`;
- every ticker has `Stop()`;
- every SQL `Rows` is closed;
- response bodies/connections are closed;
- retries use bounded timers/backoff, not unbounded goroutine spawning;
- channels/queues have fixed or externally bounded capacity;
- no unbounded in-memory history.

### 4.2 Local SQLite growth

Current local event DB contains:

- `event_outbox`: pending terminal events; successful upload removes rows;
- `task_state`: durable task baseline; one row per observed task;
- tiny metadata.

Policy:

1. **Outbox is never silently dropped.** Reliability is more important than hiding disk pressure.
2. Add diagnostics for outbox row count + file/WAL bytes.
3. Warning thresholds are configurable (initial suggestion: 10k pending rows or 50 MiB).
4. A high-water safety threshold may mark Agent health degraded, but should not invent successful delivery.
5. Add conservative `task_state` compaction for terminal tasks not seen for a long period (proposed default 180 days), only after a regression test proves it cannot replay recent terminal events.
6. SQLite free pages may be reused; do not run frequent VACUUM from the Agent service.

---

## 5. Android — low-resource native UI

### 5.1 Technology

Keep:

- Java
- Android framework Views/XML
- API 23 minimum

Do not introduce Compose, WebView, Flutter, React Native, or a permanent animation library for this redesign.

Prefer framework `FrameLayout`, `LinearLayout`, `TextView`, `ProgressBar`, `Switch`, custom lightweight `View` only when necessary.

### 5.2 Important correction to current behavior

The current Activity always applies `FLAG_KEEP_SCREEN_ON`. V2 removes this unconditional behavior.

- Portrait normal dashboard: screen follows normal Android policy.
- Landscape Focus Mode: optional `桌面显示模式` may keep screen on only while that setting is enabled and the Activity is foreground.
- Optional dim-after-idle behavior can reduce heat and panel retention.

### 5.3 Android leak rules

- no static `Activity`, `View`, `Context` (except application context) or listener ownership;
- all `Handler` callbacks removed on stop/destroy as appropriate;
- `onDestroy()` removes callbacks/messages, shuts down TTS, and shuts down/cancels executor work;
- network result callbacks carry an Activity generation token; results from the pre-rotation Activity are discarded;
- every `HttpURLConnection` is disconnected in `finally`;
- connect/read timeouts remain bounded;
- no unbounded event/task list in memory;
- orientation recreation must not create duplicate poll loops;
- no `Timer`/thread survives Activity destruction;
- TTS listener does not retain an obsolete Activity;
- drawables are XML/vector/procedural where possible; avoid large bitmaps.

### 5.4 Connection efficiency

Current requests are bounded by 2.5 s connect/read timeouts and disconnect in `finally`, which is good for leak safety. During UI implementation, benchmark whether forcing `Connection: close` is still desirable; allowing platform keep-alive may reduce repeated TCP setup CPU while still keeping each request object scoped and disconnected.

### 5.5 Android memory checks

Automated/emulator gates where practical:

- 100+ portrait/landscape rotations;
- repeatedly enter/leave settings/detail overlays;
- 10k synthetic state renders;
- TTS init/shutdown repetition;
- connectivity fail/recover loops;
- verify one active polling loop only;
- sample PSS/native heap/Java heap before and after warm-up cycles;
- fail on a clear monotonic leak, not ordinary GC saw-tooth behavior.

---

## 6. Hub SQLite — current growth analysis

Current Hub schema behavior:

- `agents`: one row per agent -> bounded for the current deployment;
- `snapshots`: one row per agent via UPSERT -> bounded;
- `events`: append-only terminal event log -> **unbounded today**;
- WAL: bounded only by normal SQLite checkpoint behavior and reader activity.

Therefore the long-term storage risk is not normal state polling; it is retained event history (plus backups if an operator accumulates them indefinitely).

---

## 7. Hub retention policy

### 7.1 Proposed defaults

Add configurable retention settings; proposed starting values:

```text
HUD_HUB_EVENT_RETENTION_DAYS=90
HUD_HUB_EVENT_RETENTION_MIN=10000
HUD_HUB_EVENT_RETENTION_MAX=100000
HUD_HUB_MAINTENANCE_INTERVAL_HOURS=6
```

Meaning:

- normally retain 90 days;
- never prune below the newest 10k events solely because they are old;
- prevent unlimited growth by retaining at most roughly 100k events under unusually high volume;
- values remain configurable for operators who want longer history.

The exact defaults should be validated against a synthetic size benchmark before freezing the release contract.

### 7.2 Batched pruning

Never delete a huge history in one transaction.

- add an index suitable for retention scans, e.g. `(agent_id, received_at, seq)`;
- delete in small batches (e.g. 500–1000 rows);
- yield between batches;
- maintenance runs outside request critical sections where possible;
- failures are logged and retried later; they do not stop the Hub.

### 7.3 Cursor correctness after pruning

Retention must not silently pretend that an old Android cursor still has complete history.

Extend the event-page response with optional retention metadata while keeping schema v1 forward compatible, for example:

```json
{
  "schemaVersion": 1,
  "events": [],
  "nextAfter": 123,
  "latestSeq": 456,
  "oldestSeq": 300
}
```

New Android behavior:

- if saved cursor is earlier than the retention floor, record that a history gap occurred;
- rebase to the retained range/high-water according to policy;
- do not individually speak old retained backlog;
- optionally show one localized informational status such as `部分历史事件已过期`;
- never fabricate the missing events.

Older Android versions ignore the optional field and remain protocol-compatible.

---

## 8. SQLite read/write performance strategy

### 8.1 Is long-term SQLite reading a problem?

Not by itself.

For this workload SQLite is a good fit because:

- one primary Agent;
- one/few Android readers;
- small requests;
- indexed cursor queries;
- WAL mode;
- short transactions.

The current `idx_events_agent_seq(agent_id, seq)` keeps cursor pagination efficient as history grows. The current single DB connection + mutex serializes access and uses very little memory; at current traffic this is a stability advantage, not a bottleneck.

Do **not** introduce a connection pool just because the DB is long-lived. Only change concurrency after a benchmark proves contention.

### 8.2 State/health read cache

Current `/state` and `/health` load and decode the same single snapshot from SQLite on every request. Android polling makes this unnecessary repeated DB/JSON work.

Add a tiny in-memory cache:

- cache only latest canonical snapshot + last-seen metadata;
- populate once from SQLite at startup;
- update only after a successful state/heartbeat commit;
- `/state` and `/health` read from the immutable cache;
- SQLite remains the restart/durability authority;
- cache is bounded to one current snapshot per configured Agent.

This removes most steady-state SQLite reads without meaningful memory cost.

### 8.3 SQLite maintenance

Automatic, low-impact maintenance:

- WAL mode stays enabled;
- keep short transactions;
- set/benchmark a modest SQLite page cache instead of an unbounded application cache;
- `PRAGMA optimize` after scheduled retention maintenance;
- periodic `wal_checkpoint(PASSIVE)` after maintenance;
- consider `journal_size_limit` (e.g. 16–32 MiB) after benchmark;
- keep the existing busy timeout;
- retain the single writer/connection model initially.

Do **not** run frequent automatic full `VACUUM`.

After DELETE, SQLite reuses free pages, so the DB file may not shrink immediately but should stop growing once retention reaches steady state. Physical shrink is an explicit maintenance operation during a low-traffic window.

### 8.4 Storage diagnostics

Add a read-only CLI command rather than a public LAN endpoint, for example:

```text
ai-control-hub stats
```

Report only non-sensitive metadata:

- database bytes;
- WAL bytes;
- event count;
- oldest/newest retained seq/time;
- SQLite page count / freelist count;
- approximate reusable bytes;
- configured retention policy.

The LAN doctor may surface warning status from these values without exposing event payloads.

---

## 9. Hub memory/goroutine/FD leak defense

### 9.1 Existing strengths to preserve

- HTTP read/write/header/idle timeouts are already bounded.
- SQLite max open connections is one.
- DB rows are scoped/closed.
- discovery/server shutdown is context-driven.

### 9.2 New regression gates

Add a synthetic long-running/accelerated test suite that does not require a real device:

1. start Hub;
2. upload thousands/tens of thousands of state/heartbeat/event operations;
3. repeatedly read state/health/events;
4. force reconnect/error paths;
5. run maintenance/pruning;
6. sample Linux `/proc` RSS, FD count and thread count;
7. in Go tests, sample `runtime.NumGoroutine()` around repeated start/stop cycles;
8. close/reopen SQLite repeatedly;
9. require metrics to settle near the post-warm-up baseline.

Nightly/extended CI can run longer than PR CI.

### 9.3 No production pprof exposure by default

Do not expose Go `net/http/pprof` on the LAN Hub. If profiling is needed, enable it only in a test build or an explicitly configured loopback-only diagnostic listener.

---

## 10. Backup and disk-growth policy

Backups are separate files and can consume more disk than the active DB if an operator schedules them indefinitely.

If automatic backup scheduling is added later, require rotation, for example:

- 7 daily backups;
- 4 weekly backups;
- explicit monthly archives if desired.

Never auto-delete the only known-good backup.

Before creating a backup, optionally warn if free disk is below a safe multiple of current DB size.

---

## 11. Visual UI plan (unchanged goals, revised implementation)

### Android portrait

Detailed dashboard:

1. connection/AUTO address;
2. overall Agent state;
3. current task/activity;
4. ZCode summary;
5. CommandCode plan/remaining/usage windows;
6. recent events;
7. TTS controls;
8. compact app/cursor diagnostics.

### Android landscape

Focus Mode only:

1. slim connection header;
2. CommandCode remaining hero card;
3. current task/activity hero card;
4. small TTS/status strip.

No full event list/settings/diagnostic grid.

### Windows

Full operator console, but native/event-driven. Visual reference remains the approved desktop mock; first implementation prioritizes correct data hierarchy and low resource use over reproducing every decorative glow.

---

## 12. Revised implementation roadmap

### R0 — Resource baseline & budgets

- Add reproducible process-resource sampling scripts.
- Record baseline RSS/PSS/CPU/handles/FDs/goroutines for current Agent/Hub/Android.
- Define regression thresholds from real release-build baseline.

### R1 — Hub storage retention

- Event retention config.
- Retention-friendly index.
- Batched event pruning.
- Optional `oldestSeq` retention floor metadata.
- Android forward-compatible retention-gap behavior tests.

### R2 — SQLite efficiency & storage diagnostics

- Latest state/heartbeat in-memory read cache.
- `ai-control-hub stats`.
- `PRAGMA optimize` / passive checkpoint maintenance.
- WAL/DB size regression tests.

### R3 — Agent local DB hygiene

- Outbox size/count diagnostics and warnings.
- Conservative terminal `task_state` retention/compaction design.
- No silent event dropping.

### R4 — Leak/endurance gates

- Go Hub goroutine/RSS/FD accelerated soak.
- Go Agent goroutine/RSS/SQLite accelerated soak.
- Windows native UI leak harness once UI exists.
- Android rotate/render/network/TTS lifecycle soak.

### UI0 — Design baseline

- Three approved reference screens.
- Shared design tokens and state semantics.

### UI1 — Android resource/i18n/lifecycle foundation

- Move strings/colors/dimens/styles into resources.
- `zh-CN` + English fallback.
- Remove unconditional keep-screen-on.
- Add lifecycle generation/cancellation safeguards.
- Preserve existing portrait functionality.

### UI2 — Android portrait redesign

- Implement detailed portrait dashboard.
- Keep existing schema-v1/event/TTS semantics.
- Add render/state tests.

### UI3 — Android landscape Focus Mode

- Remove portrait-only manifest lock.
- Add `layout-land` sparse two-hero-card layout.
- Optional desk-display keep-screen-on mode.
- Rotation/leak tests.

### UI4 — Windows native shell

- Create C++20 Win32/Direct2D/DirectWrite `ai-control-agent-ui.exe`.
- Tray + localization + native theme tokens.
- Read-only local state/health/diagnostics first.
- No service-control privileges required for normal dashboard.

### UI5 — Windows operational dashboard

- Current task/activity.
- Source details.
- CommandCode credit/windows.
- Diagnostics/event views using bounded datasets.

### UI6 — Windows privileged actions

- UAC-separated service start/stop/restart/upgrade.
- No credential editor in dashboard.
- Explicit confirmation for disruptive operations.

### UI7 — Native Mini HUD / polish

- Always-on-top compact window.
- Update only on data changes.
- Reduced motion/accessibility/window persistence.

### UI8 — Delivery/resource gates

- Windows native UI artifacts.
- Android portrait/landscape APK.
- Memory/CPU/leak budgets recorded and enforced.
- Repository/APK secret scans remain active.

---

## 13. Priority decision

The new order is:

```text
R0 -> R1 -> R2 -> R3 -> R4
                |
                +-> UI1 -> UI2 -> UI3
                +-> UI4 -> UI5 -> UI6 -> UI7
                                  -> UI8
```

Do not postpone storage retention until the database is already large. The Hub is always-on, so its data lifecycle is a foundation concern.

Android UI work may begin after R0/R1 contracts are stable; it does not need to wait for every later optimization. Windows GUI should begin only after the native resource budget and leak-test harness are defined, so the implementation cannot drift back toward a heavyweight browser UI.

---

## 14. Explicit non-goals

- Hub GUI / browser dashboard
- Electron/WebView2 production desktop UI
- Compose/Flutter/React Native Android rewrite
- continuously animated HUD graphics
- automatic deletion of pending Agent events
- silent event-history loss after Hub retention pruning
- public pprof/debug endpoints
- speculative database joins or fabricated quota/task values
- frequent automatic `VACUUM`
