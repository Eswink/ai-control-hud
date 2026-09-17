# Go Hub Runtime Migration

Status: H8 migration complete; H9 Python Central-Hub runtime retired

## Why this migration exists

The first Central Hub implementation used Python/FastAPI and declared Python `>=3.10`. The real CentOS host provides Python 3.9.19. Rather than changing the host runtime merely for this service, the production Hub was moved to Go.

The deployed runtime is one prebuilt, statically linked Linux binary:

```text
/usr/local/lib/ai-control-hub/ai-control-hub
```

CentOS does not need Python, pip, a virtual environment, or a Go compiler to install or run the Hub.

## Compatibility contract

The Go implementation was a runtime replacement, not a protocol redesign.

It preserves:

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
- Windows `auto://lan` and Android `http://auto.lan` identities.

## SQLite compatibility

The Go store intentionally retained the original Central Hub schema:

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

Existing databases created by the former Python Hub can be opened in place; an export/import is not required merely because the runtime changed.

## Go implementation boundaries

The Hub lives in the existing Go module under:

```text
agent/internal/hub/
agent/cmd/ai-control-hub/
```

This lets the Hub reuse the canonical schema-v1 `internal/domain` model, semantic event model, and pure-Go `modernc.org/sqlite` driver already used by the Agent. It avoids a second independently maintained copy of the state contract.

The standalone binary provides:

```text
ai-control-hub version
ai-control-hub serve --host HOST --port PORT
ai-control-hub backup --database PATH --output PATH
```

## Deployment cutover

The hardened systemd layout was retained, while `ExecStart` moved from Python/uvicorn to:

```text
/usr/local/lib/ai-control-hub/ai-control-hub serve --host ... --port ...
```

The installer consumes a prebuilt binary using `--binary PATH`. It keeps the existing root-only environment, unprivileged service user, persistent SQLite directory, systemd hardening, token rotation, LAN-auto mode, and removal semantics.

During migration from a prior Python installation, the installer preserves `/var/lib/ai-control-hud` and removes only the obsolete app virtual environment.

## Backup cutover

Production backup is provided only by the Go binary. It performs a live SQLite backup using `VACUUM INTO`, checks `PRAGMA integrity_check`, writes mode `0600`, fsyncs, and atomically publishes the destination.

The former Python backup console script and helper were removed in H9 so there is one supported backup implementation.

## CI gates

The Go Hub CI gate covers:

- Go Hub `vet` and tests;
- state/heartbeat/stale parity cases;
- event idempotency/cursor/primary-agent cases;
- SQLite close/reopen persistence;
- live backup/integrity/overwrite behavior;
- UDP discovery responder test;
- reproducible static Linux/amd64 build with `CGO_ENABLED=0`;
- actual binary `/health` runtime smoke;
- hardened systemd render smoke with no Python/uvicorn/venv runtime;
- binary-only deployment bundle generation;
- a Go-only-Hub guard that fails if retired Python Central-Hub paths or backup entrypoints are reintroduced.

Retained Python CI covers only legacy local diagnostics/source-verification tooling. It no longer exercises or packages a Python Central Hub.

## Field cutover result

The core production path was accepted on real devices on 2026-09-17:

- standalone Go Hub started successfully on the CentOS host;
- TCP Hub health and UDP 8788 discovery worked after the LAN firewall rule was enabled;
- Windows `auto://lan` resolved the real Hub and the SCM Agent uploaded schema-v1 state;
- ZCode and operator-supplied CommandCode credentials worked through the Windows service;
- Android auto-discovery, dashboard state, resolved URL display, and TTS worked against the Go Hub;
- stopping the Windows Agent produced Hub stale/degraded projection and restarting it returned the state to fresh/live.

The operator explicitly chose not to continue the remaining disaster-recovery field drills. Hub-outage catch-up, CentOS reboot, DHCP re-address, and backup/restore drills are therefore recorded as `NOT RUN`, not failures, and do not block continued development.

CommandCode API keys remain operator-supplied/manual; they are not auto-retrieved by the project.

## H9 retirement decision

The original retirement rule required a separate cleanup iteration after Go CI and real-device cutover. That separation has now been preserved:

1. H8 introduced the Go runtime while keeping the Python Hub as a temporary reference.
2. The core real-device Go cutover was accepted.
3. H9 removes the Python Central Hub package, Python Hub API/events/discovery tests, and the Python Hub backup entrypoint.
4. H9 adds a CI guard so the repository has one Central Hub runtime going forward.

Historical migration documentation remains because old SQLite databases and older deployed Python installations can still be encountered during upgrades. The executable Python Hub implementation itself is no longer maintained.
