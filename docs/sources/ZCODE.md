# ZCode source evidence

Status: partially verified against a sanitized discovery report from the target Windows machine (2026-09-15).

## Verified local sources

The target machine reports two ZCode SQLite databases:

- `~/.zcode/cli/db/db.sqlite`
- `~/.zcode/v2/tasks-index.sqlite`

The CLI binary was not found by the first discovery pass, so v1 deliberately does not depend on invoking ZCode.

## Selected v1 source

`~/.zcode/v2/tasks-index.sqlite`, table `tasks`.

Verified columns used by the adapter:

- `workspace_key` (TEXT, composite primary key)
- `workspace_path` (TEXT)
- `task_id` (TEXT, composite primary key)
- `title` (TEXT)
- `task_status` (TEXT, nullable)
- `updated_at` (INTEGER)
- `pinned` (INTEGER)
- `archived` (INTEGER)
- `deleted` (INTEGER)

The adapter opens the database with SQLite URI `mode=ro`, verifies this schema before querying, filters archived/deleted records, orders pinned/recent tasks first, and applies a bounded result limit.

## Canonical mapping

| Canonical field | ZCode source | Status |
| --- | --- | --- |
| `id` | stable composition/hash of `workspace_key` + `task_id` | derived |
| `title` | `title` | verified |
| `workspace` | basename of `workspace_path`, fallback `workspace_key` | derived/privacy-preserving |
| `status` | conservative mapping of `task_status` | partially verified |
| `updatedAt` | `updated_at`, converted by Unix timestamp magnitude | derived; exact unit not yet sampled |
| `startedAt` | unavailable in selected table | unavailable in v1 |
| `durationSeconds` | unavailable in selected table | unavailable in v1 |
| `activity` | unavailable in selected table | unavailable in v1 |
| `changes` | not joined across databases in v1 | unavailable in v1 |

Unexpected `task_status` values map to `unknown`; they never silently become `completed` or `failed`.

## Additional verified tables for later enrichment

`~/.zcode/cli/db/db.sqlite` exposes useful candidates including:

- `session`: title/workspace metadata plus `summary_additions`, `summary_deletions`, `time_created`, `time_updated`;
- `session_target`: target status, token/time budgets, active run timestamps;
- `model_usage`: request status, model/provider, timings and token counts;
- `tool_usage`: tool name/status/timings/exit code;
- `turn_usage`: per-turn model/tool/token aggregate data;
- workflow tables for workflow activity/run state.

These are intentionally not joined into the task list yet because the first discovery pass did not read rows and therefore did not prove the cross-table/task identifiers. A later evidence pass can add enrichment without changing the Android contract.

## Failure behavior

- missing task index -> source error `ZCode task index not found`;
- missing required table/columns -> `Unsupported ZCode task index schema`;
- SQLite read error -> sanitized `ZCode task index read failed`;
- runtime retains last-known-good data as `stale` after a later collection failure.

No task message/prompt body is read by this adapter.
