# Development

## Python backend / mock API

Requires Python 3.10+.

```bash
python -m venv .venv
source .venv/bin/activate
pip install -e '.[dev]'
pytest -q
python -m server
```

The mock server listens on `0.0.0.0:8787` and serves:

```text
GET /api/v1/state
GET /api/v1/health
```

By default it loads `server/tests/fixtures/healthy.json`. Select another committed fixture with:

```bash
HUD_FIXTURE=task_failed.json python -m server
```

The HTTP handlers read only the in-memory `SnapshotStore`; vendor adapters must never be called synchronously from request handling.

## Current phase

The mock backend is intentionally independent of the real ZCode and CommandCode integrations. Source discovery and verification happen separately before production adapters are implemented.
