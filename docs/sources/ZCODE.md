# ZCode source evidence

Status: Goal mode was verified on the target Windows machine through live API comparison with the ZCode Desktop Goal UI (2026-09-15). Ordinary-session activity is additionally guarded by schema/unit regression tests and must be revalidated against the target machine whenever the local ZCode runtime schema changes.

## Verified local sources

The target machine exposes two useful SQLite databases:

- `~/.zcode/cli/db/db.sqlite` — primary read-only runtime source for live Goal/session state;
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

### `model_usage`

The runtime DB also records request/session activity in `model_usage`. The live ordinary-session fallback uses only:

- `session_id`
- `status`
- `started_at`

Provider/model diagnostics may additionally inspect `provider_id` and `model_id`. Credential/header values are not read or exported.

## Live-state rules

A Goal is considered live only when its `active_run_last_seen_at` heartbeat is fresh. The default freshness window is 120 seconds and can be adjusted with `HUD_ZCODE_GOAL_HEARTBEAT_SECONDS`.

Historical sessions are not allowed to become `running` merely because an old `session_target.status` or `todo.status` still contains a running-like value. This rule was added after target-machine validation showed old sessions from several days earlier retaining stale running markers.

### Ordinary-session live fallback

When no fresh Goal task is available, the collector checks the newest `model_usage` row for each ZCode runtime session and joins it to the sanitized `session` title/workspace metadata.

An ordinary session is surfaced as a live task only when all of the following are true:

1. the newest request row for that session has an explicit running/waiting-like status;
2. `started_at` is fresh (10 minutes by default; a larger configured Goal heartbeat may extend this up to one hour);
3. the session row exists and passes the expected schema checks.

The newest row wins before status filtering. Therefore a newer `completed`/`failed` request suppresses an older stale `running` row instead of resurrecting it. Source health alone is never converted into a synthetic running task, and a recent completed request is not treated as active.

The emitted ordinary-session task keeps only canonical, privacy-preserving fields:

- `id`: SHA-256-derived opaque ID; raw ZCode session IDs are not exported;
- `title`: `session.title`, with a generic fallback only when the title is empty;
- `workspace`: basename of `session.path` / `session.directory`, never the full path;
- `status`: canonical `running`/`waiting` mapping from the explicit latest request status;
- `startedAt` / `updatedAt`: the latest request `started_at`;
- `durationSeconds`: elapsed time from that request start.

No prompt/message content or provider credentials are exposed by this path.

If neither a fresh Goal nor a trustworthy ordinary runtime session exists, the composite adapter falls back to `~/.zcode/v2/tasks-index.sqlite` for best-effort task/history state.

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

## Target-machine validation

The live HUD API was compared with the running ZCode Desktop Goal panel. It correctly resolved the current Goal title, workspace, current cycle/todo activity and a single `running` Goal. Older sessions that still contained running-like database values were filtered once heartbeat freshness was enforced.

Ordinary-session runtime detection is intentionally more conservative than Goal detection because there is no Goal heartbeat. It relies on the latest explicit request status plus freshness and is covered by regression tests for active, terminal-superseding, and stale rows.

## Historical fallback: task index

`~/.zcode/v2/tasks-index.sqlite`, table `tasks`, remains available as a fallback for history and for installations where the ordinary runtime signal is unavailable.

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

The adapter opens this database with SQLite URI `mode=ro`, verifies schema before querying, filters archived/deleted rows and hashes raw IDs before returning them through the canonical state API.

The task index is explicitly not treated as authoritative live-running evidence because it can lag active runtime state.

## Provider/model evidence

`model_usage` records `session_id`, `provider_id`, `model_id`, request status/timing and token counts. The sanitized discovery tool can correlate the freshest active Goal session to its actual provider/model without exporting the session ID, prompts, messages or credentials.

## Failure behavior

- unsupported Goal schema -> try ordinary runtime-session detection, then task-index fallback;
- unsupported `model_usage`/session runtime schema -> skip ordinary runtime detection and use task-index fallback;
- runtime SQLite read error -> sanitized source error / last-known-good becomes `stale`;
- missing task-index schema -> explicit unsupported-schema error;
- no trustworthy live signal -> historical task-index state rather than fabricated running activity.

All reads are read-only. The adapter does not modify ZCode state or emit provider credentials to the canonical API, Windows UI, or Android.
