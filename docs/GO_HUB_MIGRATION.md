# Go Hub Runtime Migration

Status: H8 implementation and cutover plan

## Why this migration exists

The first Central Hub implementation used Python/FastAPI and declared Python `>=3.10`. The real CentOS host currently provides Python 3.9.19. Rather than changing the host runtime merely for this service, the production Hub is being moved to Go.

The target deployment is one prebuilt, statically linked Linux binary:

```text
/usr/local/lib/ai-control-hub/ai-control-hub
```

CentOS must not need Python, pip, a virtual environment, or a Go compiler to install or run the Hub.

## Compatibility contract

The Go implementation is a runtime replacement, not a protocol redesign.

It must preserve:

- authenticated `POST /api/v1/agent/state`;
- authenticated `POST /api/v1/agent/heartbeat`;
- authenticated `POST /api/v1/agent/events`;
- schema-v1 `GET /api/v1/state`;
- schema-v1 `GET /api/v1/health`;
- event schema-v1 `GET /api/v1/events?after=&limit=`;
- stale projection after the configured heartbeat threshold;
- global monotonic event `seq` values;
- idempotency by globally unique `eventId`;
- Android first-baseline and lower-high-water silent rebase behavior;
- H7 UDP LAN discovery protocol on port 8788;
- current Windows `auto://lan` and Android `http://auto.lan` identities.

## SQLite compatibility

The Go store intentionally uses the existing Python Hub schema unchanged:

```text
agents(agent_id, agent_version, last_seen_at, last_sent_at)
snapshots(agent_id, received_at, sent_at, state_json)
events(seq, event_id, agent_id, event_type, occurred_at, received_at, event_json)
idx_events_agent_seq(agent_id, seq)
```

The default database path remains:

```text
/var/lib/ai-control-hud/hub.sqlite3
```

Existing databases must be opened in place rather than exported/imported just to change runtimes.

## Go implementation boundaries

The Hub lives in the existing Go module under:

```text
agent/internal/hub/
agent/cmd/ai-control-hub/
```

This lets the Hub reuse the canonical schema-v1 `internal/domain` model, semantic event model, and pure-Go `modernc.org/sqlite` driver already used by the Agent. It avoids a second independently maintained Go copy of the state contract.

The standalone binary provides:

```text
ai-control-hub version
ai-control-hub serve --host HOST --port PORT
ai-control-hub backup --database PATH --output PATH
```

## Deployment cutover

The hardened systemd layout is retained, but `ExecStart` changes from Python/uvicorn to:

```text
/usr/local/lib/ai-control-hub/ai-control-hub serve --host ... --port ...
```

The installer consumes a prebuilt binary using `--binary PATH`. It keeps the existing root-only environment, unprivileged service user, persistent SQLite directory, systemd hardening, token rotation, LAN-auto mode, and removal semantics.

During migration from a prior Python installation, the installer preserves `/var/lib/ai-control-hud` and removes only the obsolete app virtual environment.

## Backup cutover

Production backup no longer calls a Python console entry point. The Go binary performs a live SQLite backup using `VACUUM INTO`, checks `PRAGMA integrity_check`, writes mode `0600`, fsyncs, and atomically publishes the destination.

The older Python backup code remains only while the Python implementation is retained for parity testing.

## CI gates

H8 is code-complete only when all of these are green on the same head:

- Go Hub `vet` and tests;
- state/heartbeat/stale parity cases;
- event idempotency/cursor/primary-agent parity cases;
- SQLite close/reopen persistence;
- live backup/integrity/overwrite behavior;
- UDP discovery responder test;
- reproducible static Linux/amd64 build with `CGO_ENABLED=0`;
- actual binary `/health` runtime smoke;
- hardened systemd render smoke with no Python/uvicorn/venv runtime;
- binary-only H3 validation bundle generation;
- existing Go Agent and Android regression CI.

## Python retirement rule

Do not delete the Python Hub in the same step that introduces the Go Hub. Keep it as a reference until:

1. Go CI/parity gates are green;
2. the Go binary runs on the actual CentOS host;
3. Windows uploads state/events successfully to the Go Hub;
4. Android state/events/TTS behavior is validated against the Go Hub;
5. reboot, outage, DHCP rediscovery, and backup/restore field checks pass.

After those gates pass, remove the Python production Hub path and its deployment/backup dependencies in a separate, reviewable cleanup iteration.
