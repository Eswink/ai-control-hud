# ZCode source evidence

Status: Goal mode was verified on the target Windows machine through live API comparison with the ZCode Desktop Goal UI (2026-09-15). Pause/resume and ordinary-session rules are additionally covered by regression tests and should be revalidated against the target machine whenever ZCode changes its local runtime/log schema.

## Verified local sources

The target machine exposes three useful read-only source families:

- `~/.zcode/cli/db/db.sqlite` — primary runtime source for Goal/session metadata and request activity;
- `~/.zcode/cli/log/zcode-*.jsonl` — real-time turn lifecycle evidence;
- `~/.zcode/v2/tasks-index.sqlite` — recent task/history projection.

The production HUD does not invoke a ZCode CLI binary and does not write any of these sources.

## Effective storage-root resolution

ZCode storage is no longer assumed to live only under the interactive user's
`~/.zcode`. The Agent resolves one shared read-only layout for the task
collector, turn logs and provider metadata.

Resolution precedence is:

1. `HUD_ZCODE_HOME` — explicit AI Control HUD whole-root override;
2. `ZCODE_HOME` — whole ZCode root replacement (`v2/` and `cli/`);
3. the interactive user's default `~/.zcode/v2/setting.json` `dataBaseDir`
   — moves the Desktop-owned `v2` root to
   `<dataBaseDir>/.zcode/v2` while leaving the CLI runtime root at the
   interactive user's `~/.zcode/cli`;
4. `ZCODE_DATA_BASE_DIR` — compatibility fallback for the same Desktop-owned
   `v2` relocation;
5. the historical `~/.zcode` layout.

Existing explicit leaf overrides remain stronger for their own sources:
`HUD_ZCODE_RUNTIME_DB`, `HUD_ZCODE_DB`, `HUD_ZCODE_LOG_DIR` and
`HUD_ZCODE_CONFIG`.

The `dataBaseDir` control file is parsed read-only and tolerates UTF-8 BOM,
JSONC line/block comments, unknown fields, Unicode and space-containing paths.
When a custom Desktop data root is active, provider discovery checks the
effective custom `v2/config.json` first and retains the default-profile
`v2/config.json` as a compatibility candidate because current ZCode builds
have exhibited split persistence across these roots.

This resolver follows observed ZCode behavior rather than claiming a stable
vendor filesystem API. Official feedback #268/#272 documents current
`dataBaseDir` split-root failures, while `ViviQuan/ZZ-Switch` independently
implements the same `setting.json -> dataBaseDir -> <root>/.zcode/v2`
resolution and `william0wang/zcode-acp` documents `ZCODE_HOME` as a full
replacement for `~/.zcode`.

## Primary live source: runtime DB

The production adapter reads `~/.zcode/cli/db/db.sqlite` in SQLite read-only mode.

Verified tables/columns used for Goal mode:

### `session_target`

- `session_id`
- `objective`
- `status`
- `time_used_seconds`
- `time_updated`
- `summary_title`
- `active_run_started_at`
- `active_run_last_seen_at`

### `todo`

- `session_id`
- `content`
- `status`
- `position`

### `session`

- `id`
- `directory`
- `path`
- `title`
- `summary_additions`
- `summary_deletions`

### `model_usage`

The runtime DB also records request/session activity in `model_usage`. The conservative DB-only ordinary-session fallback uses only:

- `session_id`
- `status`
- `started_at`

Provider/model diagnostics may additionally inspect `provider_id` and `model_id`. Credential/header values are not read or exported.

## Live-state rules

### Goal mode

A Goal is considered live only when its `active_run_last_seen_at` heartbeat is fresh. The default freshness window is 120 seconds and can be adjusted with `HUD_ZCODE_GOAL_HEARTBEAT_SECONDS`.

A fresh Goal target whose normalized target status is `running`/`active` remains canonical `running` even if the current todo projection is still `pending`/`waiting`. This matters after pause/resume: the Goal runner and heartbeat can already be active again while the todo row has not yet advanced. A running todo also implies `running`; waiting is used only when there is no stronger running signal.

Historical sessions are not allowed to become `running` merely because an old `session_target.status` or `todo.status` still contains a running-like value. Without a fresh Goal heartbeat, only recent terminal Goal state can remain visible.

### Turn lifecycle log

For ordinary non-Goal work, the collector supplements the runtime DB with the newest bounded ZCode JSONL log tails. It reads at most the newest two `zcode-*.jsonl` files and at most 512 KiB from each file.

The canonical lifecycle is:

```text
turn.started
    -> model/tool/session activity
    -> turn.completed | turn.failed | turn.cancelled
```

A fresh unmatched `turn.started` is authoritative evidence that the session is still running. Events for the same open turn refresh its activity timestamp. A terminal event closes the foreground turn and prevents an older `model_usage=running` row from resurrecting it. A later `turn.started` reopens the same session after pause/cancel/resume.

### ZCode 3.14 background workflows

Target-machine evidence captured on 2026-09-20 shows that current ZCode 3.14
CLI JSONL uses a lower-level structured-log lifecycle for background jobs:

```text
background_task.tracking.started
    -> background task work
    -> background_task.tracking.terminal

background_task.notification.enqueued
background_task.notification.runtime_enqueued
```

The first pair is session-scoped liveness. The notification events are delivery
bookkeeping after/around task completion and are **not** treated as running
work by themselves.

The collector therefore combines two compatibility families:

1. current real structured-log `background_task.tracking.started|terminal`
   events, tracked as a bounded per-session open-count;
2. the previously observed protocol-compatible
   `session.updated {taskId,status}` / `turn.started
   {inputSource:"background_task"}` family, retained as a compatibility path.

Consequences:

- each tracking start increments the parent session's live background count;
- each tracking terminal decrements it without affecting another concurrent
  background job;
- the parent session remains running after a foreground `turn.completed`
  while the background count is non-zero;
- notification enqueue events never fabricate liveness on an otherwise idle
  session;
- sub-agent/background identities remain internal and do not become top-level
  HUD tasks;
- when a lagging Goal projection says completed/failed but fresh lifecycle
  evidence says background work is still live, lifecycle wins for status while
  Goal metadata remains the title/workspace/activity enrichment.

The reader accepts historical JSONL with top-level `event`, current typed
envelopes with top-level `type`, and verified nested `payload`/`context`
session/timestamp placement.

This mapping is corroborated by the target machine itself and by current public
reverse-engineering in `Mnehmos/mnehmos.zcode.mcp`, which documents
`background_task.tracking.started|terminal`,
`background_task.notification.enqueued|runtime_enqueued`, the
`session/cancelBackgroundTask` control surface, and a session projection
containing `backgroundJobs[]`. `william0wang/zcode-acp` and
`tizerluo/zcode-open-bridge` remain useful corroboration for the higher-level
session-event compatibility family.

The default open-turn/background freshness window is 30 minutes and can be adjusted with `HUD_ZCODE_TURN_FRESH_SECONDS` (bounded to four hours). The log directory normally derives from the configured runtime DB (`.../cli/db/db.sqlite` -> `.../cli/log`) and can be overridden with `HUD_ZCODE_LOG_DIR` for interactive/non-service use.

The JSONL reader consumes only event/session/timestamp metadata. Prompt/message bodies, tool payloads, credentials and headers are not exported.

### Ordinary-session DB fallback

If no usable live turn-log state is available, the collector checks the newest `model_usage` row for each runtime session and joins it to sanitized `session` title/workspace metadata.

A DB-only ordinary session is surfaced only when:

1. the newest request row has an explicit running/waiting-like status;
2. `started_at` is fresh (10 minutes by default; a larger configured Goal heartbeat may extend this up to one hour);
3. the session row exists and passes schema checks;
4. no newer terminal turn-log state closes that request.

The newest request row wins before status filtering, so a newer completed/failed request suppresses an older DB running row. Source health alone is never converted into a synthetic running task.

The emitted ordinary-session task keeps only canonical, privacy-preserving fields:

- `id`: SHA-256-derived opaque ID; raw ZCode session IDs are not exported;
- `title`: `session.title`, with a generic fallback only when empty;
- `workspace`: basename of `session.path` / `session.directory`, never the full path;
- `status`: canonical `running`/`waiting`;
- `startedAt` / `updatedAt`: turn timestamps when available, otherwise latest request start;
- `durationSeconds`: elapsed time from the selected start.

## Canonical Goal mapping

| Canonical field | ZCode source | Status |
| --- | --- | --- |
| `id` | SHA-256-derived stable ID from `session_id` | derived/privacy-preserving |
| `title` | `session.title`, then `session_target.summary_title`, then `objective` | verified |
| `workspace` | basename of `session.path` / `session.directory` | derived/privacy-preserving |
| `status` | fresh heartbeat + target/todo state | verified on live Goal |
| `startedAt` | `session_target.active_run_started_at` | verified |
| `updatedAt` | `active_run_last_seen_at`, fallback `time_updated` | verified |
| `durationSeconds` | `session_target.time_used_seconds`, fallback elapsed start time | verified |
| `activity` | first running/waiting `todo.content` | verified |
| `changes` | `session.summary_additions` / `summary_deletions` when present | schema verified; may be null |

Timestamp conversion accepts Unix seconds/milliseconds/microseconds by magnitude because ZCode stores integer timestamps across local tables.

## Recent task/history merge

`~/.zcode/v2/tasks-index.sqlite`, table `tasks`, remains the best source for the recent task list and completed/failed history, but it is not authoritative for live status because its projection can lag.

Verified fields include:

- `workspace_key`
- `workspace_path`
- `task_id`
- `title`
- `task_status`
- `updated_at`
- `pinned`
- `archived`
- `deleted`

When a trustworthy live Goal or ordinary runtime task exists, the collector now merges recent task-index history instead of returning only the live source. Before the merge it excludes rows whose raw `task_id` matches the live runtime session ID, so a stale task-index status cannot duplicate or override the live task. Raw IDs remain internal; emitted task-index IDs are hashed.

The resulting `zcode.summary` therefore contains the live running/waiting count plus recent completed/failed task-index counts. Display rows remain bounded by `HUD_ZCODE_TASK_LIMIT`; summary counting is not limited to the displayed row count.

If no trustworthy live/runtime task exists, the task index remains the best-effort fallback state.

## Corroborating public implementations/protocol evidence

The pause/resume and ordinary-turn rules were cross-checked against public ZCode integrations rather than inferred from source health:

- [`Oct1AtJoe/zcode-desktop`](https://github.com/Oct1AtJoe/zcode-desktop) reads the ZCode task index and JSONL log, and explicitly uses live `turn.started`/terminal state to override lagging task projection state;
- [`william0wang/zcode-acp`](https://github.com/william0wang/zcode-acp) documents the turn lifecycle ending in `turn.completed`, `turn.failed` or `turn.cancelled` and notes projection/status staleness;
- [`open-ace/open-ace#1075`](https://github.com/open-ace/open-ace/issues/1075) documents the same turn lifecycle/state event family.

These are corroborating public implementations/protocol observations, not an assumption that third-party code is authoritative for every ZCode release.

## Privacy-safe compatibility evidence

When ZCode changes, the interactive user can capture a bounded compatibility
report without sharing prompts, session IDs, credentials or filesystem paths:

```powershell
.\ai-control-agent.exe zcode evidence
```

The JSON report contains only:

- the sanitized layout-resolution source;
- presence/readability and known-schema capability booleans for the runtime DB
  and task index;
- at most the same two newest / 512 KiB-per-file turn-log window already used
  by the production collector;
- bounded event-name counts whose keys match `[A-Za-z0-9._-]{1,64}`;
- counts of `session.updated` records carrying task/status metadata and
  `turn.started` records marked `inputSource=background_task`;
- counts of real 3.14 `background_task.tracking.started|terminal` records plus
  how many of those records contain a parseable session link; raw session IDs
  are never serialized;
- provider-config candidate/readable/parseable counts plus the total number of provider entries found, never provider IDs, URLs, keys or other provider contents.

Unknown/unsafe event names are counted only as `otherEventRecords`. Payload
text, task IDs, session IDs, full paths, provider URLs and keys are never
serialized. This command is intended to freeze a new ZCode compatibility
baseline before collector semantics are changed.

## Target-machine validation

The live HUD API was previously compared with the running ZCode Desktop Goal
panel and correctly resolved Goal title, workspace, current cycle/todo activity
and fresh-heartbeat liveness. Later target-machine reports exposed paused/resumed
Goal projection lag and hidden recent history; both are covered by regression
tests.

A 2026-09-20 ZCode 3.14 field capture from a custom `dataBaseDir` further
confirmed:

- `layoutSource=data_base_setting`;
- runtime DB, task index and turn-log directory all readable;
- Goal/runtime/task-index schemas compatible;
- two bounded turn-log files parsed successfully (1,315/1,315 records);
- real background event vocabulary:
  `background_task.tracking.started=2`,
  `background_task.tracking.terminal=2`,
  `background_task.notification.enqueued=2`, and
  `background_task.notification.runtime_enqueued=2`;
- provider candidates were visible across split roots
  (3 readable, 2 parseable, 19 provider entries);
- canonical state reported one running task plus two recent completed tasks.

That capture also showed zero `session.updated` task signals and zero
`inputSource=background_task` turn starts, which is why the production
collector now consumes the real structured-log tracking pair instead of relying
only on the earlier compatibility family.

## Provider/model evidence

`model_usage` records `session_id`, `provider_id`, `model_id`, request status/timing and token counts. The sanitized discovery tool can correlate runtime provider/model evidence without exporting session IDs, prompts, messages or credentials.

## Failure behavior

- unsupported Goal schema -> try ordinary runtime-session detection, then task-index fallback;
- JSONL log absent/unreadable -> continue with conservative DB-only ordinary-session detection;
- unsupported `model_usage`/session runtime schema -> skip ordinary DB detection and use task-index fallback;
- runtime SQLite read error -> sanitized source error / last-known-good becomes `stale`;
- history/task-index read failure while trustworthy live state exists -> retain live state rather than erasing it;
- missing task-index schema with no live state -> explicit unsupported-schema error;
- no trustworthy live signal -> historical task-index state rather than fabricated running activity.

All reads are read-only. The adapter does not modify ZCode state or emit provider credentials, raw session IDs, prompts/messages, or full filesystem paths to the canonical API, Windows UI, or Android.
