# Local source discovery

Issues #1 and #2 require facts from the machine where ZCode and Command Code are actually installed. `tools.discovery` generates a sanitized report suitable for manual review before any information is shared.

## Default safety properties

With no opt-in flags the tool:

- performs no network requests;
- does not query SQLite table rows;
- opens discovered SQLite databases in read-only mode;
- records table/view and column schema only;
- parses selected Command Code JSON files into key/type/length metadata without recording values;
- reports only whether `COMMAND_CODE_API_KEY` exists, never its value;
- omits hostname and username;
- shortens home-directory paths to `~/...` and suppresses arbitrary external parent paths;
- runs only `--version` for a discovered CLI.

## Basic run

From the repository root:

```cmd
python -m tools.discovery
```

Output defaults to:

```text
.local\discovery-report.json
```

`.local/` is ignored by Git. Review the JSON manually before sharing it.

## Explicit evidence probes

For the current Windows integration work, run both opt-in probes together:

```cmd
python -m tools.discovery --zcode-status-summary --commandcode-status
```

`--zcode-status-summary` reads only aggregate metadata from the active ZCode `tasks` rows:

- distinct `task_status` values and counts;
- total active task count;
- digit magnitude/inferred unit for `created_at` and `updated_at`.

It does **not** query or emit task titles, task IDs, prompts, message data, searchable text, workspace paths, or exact timestamps.

`--commandcode-status` invokes `cmdc status --json` (or another detected supported alias). The Command Code CLI may perform its own network/auth checks. The discovery tool keeps only JSON key/type shape plus exit-code metadata; it does not preserve the command's values or credential material.

The report's `privacy` object records when these opt-in behaviors were used.

## Nonstandard locations

Additional roots are repeatable:

```cmd
python -m tools.discovery --zcode-root D:\path\to\zcode --commandcode-root D:\path\to\commandcode
```

## Tests

The tests intentionally seed fake secret task/auth values and assert those values are absent from generated metadata, including the aggregate ZCode probe:

```cmd
python -m pytest -q tools\tests
```
