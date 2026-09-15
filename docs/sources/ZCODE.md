# ZCode source evidence

Status: verified on the target Windows machine through live API comparison with the ZCode Desktop Goal UI (2026-09-15).

## Verified local sources

The target machine exposes two useful SQLite databases:

- `~/.zcode/cli/db/db.sqlite` — authoritative source for live Goal/session state;
- `~/.zcode/v2/tasks-index.sqlite` — best-effort historical/task-index fallback.

The production HUD does not depend on invoking a ZCode CLI binary.

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

The adapter also uses `model_usage` for provider/model diagnostics; credentials are not read from this table.

## Live-state rules

A Goal is considered live only when its `active_run_last_seen_at` heartbeat is fresh. The default freshness window is 120 seconds and can be adjusted with `HUD_ZCODE_GOAL_HEARTBEAT_SECONDS`.

Historical sessions are not allowed to become `running` merely because an old `session_target.status` or `todo.status` still contains a running-like value. This rule was added after target-machine validation showed old sessions from several days earlier retaining stale running markers.

When no fresh Goal exists, the composite adapter falls back to `~/.zcode/v2/tasks-index.sqlite`.

## Canonical mapping

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

## Target-machine validation

The live HUD API was compared with the running ZCode Desktop Goal panel. It correctly resolved the current Goal title, workspace, current cycle/todo activity and a single `running` Goal. Older sessions that still contained running-like database values were filtered once heartbeat freshness was enforced.

## Historical fallback: task index

`~/.zcode/v2/tasks-index.sqlite`, table `tasks`, remains available as a fallback for non-Goal/history state.

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

The adapter opens this database with SQLite URI `mode=ro`, verifies schema before querying, filters archived/deleted rows and hashes raw IDs before returning them to Android.

The task index is explicitly not treated as an authoritative live-running source because target-machine testing showed it can lag behind an actively running Goal.

## Provider/model evidence

`model_usage` records `session_id`, `provider_id`, `model_id`, request status/timing and token counts. The sanitized discovery tool can correlate the freshest active Goal session to its actual provider/model without exporting the session ID, prompts, messages or credentials.

## Failure behavior

- unsupported Goal schema -> fall back to task index;
- runtime SQLite read error -> sanitized source error / last-known-good becomes `stale`;
- missing task-index schema -> explicit unsupported-schema error;
- no live Goal -> task-index fallback rather than fabricated running state.

All reads are read-only. The adapter does not modify ZCode state or emit provider credentials to the canonical API or Android.
