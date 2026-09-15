# Development

## Python backend

Requires Python 3.10+.

```bash
python -m venv .venv
source .venv/bin/activate
pip install -e '.[dev]'
python -m pytest -q
python -m server
```

The server listens on `0.0.0.0:8787` and serves:

```text
GET /api/v1/state
GET /api/v1/health
```

### Safe startup behavior

A normal `python -m server` starts with both vendor sources marked `disabled`. It does **not** display synthetic fixture data as live. Production adapters will be wired in by #6 and #7 after local source verification.

For Android/mock development, explicitly select a committed fixture:

```bash
HUD_FIXTURE=healthy.json python -m server
HUD_FIXTURE=task_failed.json python -m server
```

The HTTP handlers read only the in-memory `SnapshotStore`; vendor adapters are run by independent background collector tasks and are never called synchronously from request handling.

### Collector configuration

The runtime accepts conservative environment overrides:

```text
HUD_ZCODE_INTERVAL_SECONDS=1
HUD_COMMANDCODE_INTERVAL_SECONDS=60
```

Collector failures are isolated by source. If a source has a last-known-good snapshot, a later failure changes it to `stale` and retains that data. A source that has never succeeded becomes `error` with canonical data set to `null`. Unknown exception messages are not surfaced; an adapter must explicitly use `PublicAdapterError` for a sanitized diagnostic message.

## Local source discovery

Source discovery and verification remain separate from production adapters. See `docs/LOCAL_DISCOVERY.md` and run:

```bash
python -m tools.discovery
```
