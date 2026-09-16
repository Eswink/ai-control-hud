# AI Control HUD — Central Hub V2 Plan

Status: active development plan

## Objective

Keep the always-on authority on the 24/7 CentOS server while preserving local ZCode/CommandCode collection on Windows and the Android schema-v1 state contract.

The production deployment is:

```text
Windows development machine              24/7 CentOS server                  Android HUD
┌──────────────────────────┐             ┌────────────────────────────┐       ┌──────────────────────┐
│ ZCode / CommandCode      │             │ ai-control-hub (Go)        │       │ Native Java HUD      │
│          │               │   outbound  │                            │ HTTP  │                      │
│          ▼               ├────────────►│ latest canonical snapshot  ├──────►│ state dashboard      │
│ ai-control-agent (Go)    │             │ agent heartbeat            │       │ durable event cursor │
│          │               │             │ durable event log          │       │ local TTS policy     │
│          ├ local API     │             │ SQLite                     │       │ quiet hours          │
│          └ durable outbox│    UDP      │ LAN discovery :8788        │       │                      │
└──────────────────────────┘             └────────────────────────────┘       └──────────────────────┘
```

## Architectural decisions

1. **Collection stays local.** ZCode and CommandCode credentials/data sources remain on Windows.
2. **Windows pushes.** The Hub never polls Windows; heartbeat age is the machine-offline signal.
3. **Local diagnostics remain.** The Go Agent keeps local `/api/v1/state` and `/api/v1/health`.
4. **Snapshots and events remain separate.** Snapshot state is replaceable; task terminal events are durable and cursor-consumed.
5. **CentOS is Android authority.** Android reads state/events from the Hub, not directly from Windows.
6. **Notification policy is local.** Android owns TTS preferences, quiet hours, and old-event suppression.
7. **Private networking first.** Trusted LAN/Tailscale/WireGuard are supported; public exposure is not the default model.
8. **Discovery locates but does not authenticate.** UDP discovery contains no bearer token or vendor credential.
9. **Stable logical identities survive DHCP.** Windows stores `auto://lan`; Android stores `http://auto.lan`.
10. **One Central Hub runtime.** After H9, the repository maintains only the standalone Go Hub; the Python/FastAPI Central Hub is retired.
11. **CommandCode keys are operator supplied.** The project does not auto-retrieve real CommandCode API keys.

## Hub protocol

### Agent ingest

```text
POST /api/v1/agent/state
POST /api/v1/agent/heartbeat
POST /api/v1/agent/events
Authorization: Bearer <project-internal Hub token>
```

The Hub token is independent from CommandCode/ZCode credentials.

### Android reads

```text
GET /api/v1/state
GET /api/v1/health
GET /api/v1/events?after=<seq>&limit=<1..100>
```

`/api/v1/state` remains schema-v1 compatible. Before the first Windows snapshot it returns HTTP 503 while `/api/v1/health` can still return 200; this means the Hub is reachable but has no primary-agent state yet.

### Stale projection

If the primary Agent heartbeat exceeds the configured threshold, the Hub retains last trustworthy data, projects source health to `stale`, and makes aggregate state `degraded`. Recovery requires no reconfiguration.

## Event delivery semantics

1. a task transition creates a stable `eventId`;
2. the Agent writes it to a local SQLite outbox before delivery;
3. delivery is at least once;
4. Hub insertion is idempotent by globally unique `eventId`;
5. Hub assigns global monotonic `seq` values;
6. Android stores a cursor and consumes ordered pages;
7. a fresh Android install establishes a silent baseline from `latestSeq`;
8. a lower Hub high-water mark after restore/reset causes a silent rebase.

## LAN discovery

Default trusted-LAN ports:

```text
TCP 8787   Hub HTTP API
UDP 8788   Hub discovery
```

Probe:

```text
AI_CONTROL_HUD_DISCOVER_V1
```

Reply contains only service/schema/Hub ID/scheme/HTTP port/version. The client derives the physical address from the UDP responder source IP.

A missing firewall rule for UDP 8788 makes auto-discovery fail even when the Hub HTTP service itself is healthy; this was observed and corrected during real-device acceptance.

## Persistence

The Hub uses SQLite tables compatible with the original Central Hub database:

```text
agents
snapshots
events
idx_events_agent_seq
```

Default path:

```text
/var/lib/ai-control-hud/hub.sqlite3
```

Go live backup is provided by:

```text
ai-control-hub backup --database PATH --output PATH
```

It uses `VACUUM INTO`, `PRAGMA integrity_check`, mode `0600`, fsync, atomic publication, and overwrite protection.

## Iteration roadmap

### H0 — Architecture freeze

- [x] Central-Hub plan recorded.
- [x] Architecture/ADR drift corrected.

### H1 — Linux Hub MVP

- [x] SQLite-backed Hub store.
- [x] authenticated snapshot ingest.
- [x] authenticated heartbeat ingest.
- [x] schema-v1 state/health API.
- [x] stale projection.
- [x] persistence/API tests.

### H2 — Go remote uploader

- [x] bounded HTTP timeouts.
- [x] snapshot upload.
- [x] independent heartbeat.
- [x] retry/backoff.
- [x] Hub URL/agent/token configuration.
- [x] local HTTP diagnostics retained.

### H3 — Real-device cutover

Core production cutover accepted by the operator on 2026-09-17:

- [x] standalone Go Hub deployed on the real CentOS host.
- [x] trusted-LAN TCP/UDP reachability and UDP discovery validated.
- [x] Windows `auto://lan` configuration resolved the real Hub.
- [x] Windows SCM first-install path validated.
- [x] ZCode runtime/task-index paths validated under the service.
- [x] operator-supplied CommandCode credential imported to DPAPI and `doctor --live` passed.
- [x] Windows-to-Hub schema-v1 state upload validated.
- [x] Android auto-discovery/dashboard/resolved URL/TTS validated.
- [x] Windows stop -> Hub stale/degraded -> Windows restart -> fresh/live validated.
- [ ] Hub-outage durable-event catch-up — `NOT RUN` by operator choice.
- [ ] CentOS reboot/autostart drill — `NOT RUN` by operator choice.
- [ ] DHCP re-address/rediscovery drill — `NOT RUN` by operator choice.
- [ ] live backup/restore drill — `NOT RUN` by operator choice.

The four `NOT RUN` drills are not failures and no longer block further development.

### H4 — Durable event stream

- [x] independent event schema.
- [x] safe first-run baseline.
- [x] durable local SQLite outbox.
- [x] independent observation/delivery loops.
- [x] idempotent Hub event ingest.
- [x] global monotonic cursor API.
- [x] `latestSeq` high-water baseline.
- [x] persistence/retry/idempotency tests.

### H5 — Android voice notifications

- [x] event parser/client.
- [x] persistent cursor.
- [x] silent first baseline and reset rebase.
- [x] local TextToSpeech.
- [x] independent completed/failed preferences.
- [x] default 23:00–08:00 quiet hours.
- [x] old-event suppression and one catch-up summary.
- [x] Android unit/lint/APK CI.
- [x] real-device TTS behavior validated during H3.

### H6 — Deployment hardening

- [x] hardened CentOS systemd unit/installer.
- [x] config/secret/data path separation.
- [x] Go live backup/restore implementation and notes.
- [x] private-network deployment guide.
- [x] Windows Hub credential in machine-scope DPAPI.
- [x] token-file import/rotation.
- [x] deployment/secret/backup CI smoke coverage.

Remaining optional field disaster-recovery drills are documented under H3 as `NOT RUN` by operator choice.

### H7 — LAN Hub auto-discovery

- [x] UDP 8788 discovery responder.
- [x] explicit `--lan-auto`; safe loopback default retained.
- [x] discovery contains no secrets.
- [x] Windows `auto://lan`.
- [x] Windows rediscovery after request failure.
- [x] Windows `hub status` resolved URL.
- [x] Android `http://auto.lan`.
- [x] Android rediscovery and resolved URL display.
- [x] stable event cursor identity across physical-IP changes.
- [x] real Windows/Android LAN broadcast validation.
- [ ] controlled DHCP/IP-change drill — `NOT RUN` by operator choice.

### H8 — Standalone Go Hub runtime

- [x] Go Hub package and command.
- [x] protocol/SQLite parity with the former Python Hub.
- [x] Go live backup.
- [x] reproducible static Linux/amd64 build.
- [x] runtime and hardened-systemd CI smoke.
- [x] binary-only deployment bundle.
- [x] real CentOS/Windows/Android core cutover accepted.

H8 intentionally kept the Python Hub temporarily so runtime introduction and runtime retirement were reviewable separately.

### H9 — Go-only Hub cleanup

- [x] remove `server/hub/` Python Central Hub package.
- [x] remove Python Hub entrypoint and Hub-specific API/events/discovery tests.
- [x] remove Python Hub backup helper/console entrypoint.
- [x] retain Python local diagnostics/source-verification tooling that is not a Central Hub runtime.
- [x] add Go-only Hub CI guard preventing retired paths from returning.
- [x] update deployment/migration docs for a single authoritative Hub runtime.
- [ ] fresh H9 PR CI green on the final head.

Exit criterion: repository and deployment docs expose one supported Central Hub runtime (`ai-control-hub` Go binary), with the old Python Hub represented only as migration history.

## Next product work after H9

Field testing exposed one Android UX ambiguity worth fixing next: when the Hub is reachable (`/health` 200) but no primary-agent snapshot exists (`/state` 503), the Android dashboard currently collapses that state into `offline`. A follow-up iteration should distinguish **Hub reachable / waiting for Agent** from **Hub unreachable** without changing schema-v1.

## Non-goals

- controlling ZCode from the phone;
- exposing vendor credentials to Hub or Android;
- automatic retrieval of CommandCode API keys;
- public Internet deployment by default;
- discovery across arbitrary routed networks;
- multi-user RBAC;
- heavyweight message brokers;
- exactly-once delivery;
- rewriting working collectors without a measured need.

## Engineering rules

1. schema-v1 Android compatibility must not silently break.
2. source failures/offline states must never be represented as valid empty or zero data.
3. no token, API key, raw auth payload, or private log is stored in Git.
4. new network loops require bounded timeouts and cancellation.
5. every protocol change adds tests before the next layer is introduced.
6. local diagnostics remain usable when the Hub is unreachable.
7. discovery is a private-network convenience layer, never authentication.
8. Central Hub behavior has one production implementation: Go.
