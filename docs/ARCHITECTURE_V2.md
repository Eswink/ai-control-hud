# Architecture V2 — Cross-Platform Desktop Agent

Status: approved migration target as of 2026-09-16.

## Decision

The long-running desktop backend will migrate from Python to Go while preserving the Android client and the existing `schemaVersion = 1` HTTP contract.

The product is re-framed as two stable components:

```text
Desktop / workstation
┌──────────────────────────────────────────┐
│ AI Control Agent (Go)                    │
│                                          │
│  ZCode collector    CommandCode collector│
│        │                    │             │
│        └────────┬───────────┘             │
│                 ▼                         │
│            State engine                  │
│                 │                         │
│            HTTP API :8787                │
│                 │                         │
│      platform service integration        │
└─────────────────┬────────────────────────┘
                  │ LAN / Tailscale
                  ▼
┌──────────────────────────────────────────┐
│ Native Android HUD                       │
│ Java + XML Views                         │
└──────────────────────────────────────────┘
```

## Why Go

The desktop component has evolved from a development server into a long-running local agent. The migration optimizes for:

- single-binary Windows deployment;
- straightforward Linux/macOS builds;
- low idle overhead;
- explicit concurrency and cancellation using goroutines and `context`;
- simple service-manager integration;
- stable standard-library HTTP/JSON stack;
- minimal operator dependency on language runtimes or virtual environments.

Python remains the reference implementation until Go reaches semantic parity. It is not deleted during migration.

## Compatibility invariant

The Android client must not need to know that the backend language changed.

The Go implementation must preserve:

- `GET /api/v1/state`;
- `GET /api/v1/health`;
- `schemaVersion = 1`;
- source health values: `ok`, `stale`, `error`, `disabled`;
- overall status values: `live`, `degraded`;
- the rule that source failure never masquerades as a valid zero/empty value;
- current task and usage semantics documented in `docs/API.md`.

Any incompatible change requires a new schema version and is outside the migration bootstrap.

## Target repository layout

```text
agent/
├── go.mod
├── cmd/
│   └── ai-control-agent/
│       └── main.go
└── internal/
    ├── api/
    ├── collector/
    │   ├── zcode/
    │   └── commandcode/
    ├── config/
    ├── domain/
    ├── platform/
    │   ├── windows/
    │   ├── linux/
    │   └── darwin/
    ├── secrets/
    └── store/

android/                    # preserved
server/                     # Python reference during migration
docs/
```

## Domain boundary

Vendor implementation details must not leak into Android or API handlers.

```text
ZCode SQLite/log/runtime state
        │
        ▼
ZCode collector
        │
        ▼
Canonical ZCode state

CommandCode billing endpoints
        │
        ▼
CommandCode collector
        │
        ▼
Canonical usage state

Canonical state
        │
        ▼
Snapshot store
        │
        ▼
HTTP API
```

The API layer reads only committed snapshots. It must never perform a vendor database/API request on behalf of an Android HTTP request.

## Collector runtime

Collectors run independently.

Initial target cadence:

- ZCode: 1 second;
- CommandCode: 60 seconds;
- Android polling remains 2 seconds.

Each collection attempt uses a bounded `context.Context` timeout and commits atomically to the in-memory store.

Failure semantics are retained from the Python implementation:

```text
first collection fails
    -> error + null data

collection succeeds at least once, later attempt fails
    -> stale + last-known-good data

later collection succeeds
    -> ok + new snapshot
```

One collector cannot block or cancel the other.

## ZCode source strategy

The verified live Goal path remains authoritative:

```text
~/.zcode/cli/db/db.sqlite
├── session
├── session_target
├── todo
└── model_usage
```

Live Goal status is determined primarily by the freshness of `active_run_last_seen_at`; residual database `running` values are not sufficient by themselves.

Primary mapping:

- title <- `session.title`;
- workspace <- basename of session directory;
- activity <- current running/pending todo;
- duration <- `session_target.time_used_seconds`;
- changes <- `session.summary_additions` / `summary_deletions` when available;
- current freshness <- `active_run_last_seen_at`.

Fallback remains:

```text
~/.zcode/v2/tasks-index.sqlite
```

Task-index rows are history/fallback only. Old rows must not reappear as current HUD work; the existing implementation uses a bounded freshness window.

## CommandCode source strategy

The verified target account exposes the required information through CommandCode billing calls, normalized into the existing API contract.

Vendor endpoint paths remain isolated inside the CommandCode collector because they are not a stable Android/API contract.

The collector must provide, when verified and available:

- plan label;
- remaining credit;
- plan credit ceiling when semantically safe;
- 5-hour usage percentage/reset;
- weekly usage percentage/reset.

The target environment currently uses a local, gitignored provider mirror as a compatibility workaround for ZCode custom-provider persistence behavior. V2 will introduce a platform secret abstraction so credentials are no longer conceptually tied to a project JSON file.

## Configuration

Non-secret configuration belongs in a small agent config file or CLI flags. Example conceptual fields:

```json
{
  "listen": "0.0.0.0:8787",
  "zcode": {
    "refreshSeconds": 1,
    "goalHeartbeatSeconds": 120
  },
  "commandCode": {
    "refreshSeconds": 60
  }
}
```

Secret values do not belong in the normal configuration schema.

## Secrets abstraction

Define a platform-neutral boundary:

```text
SecretStore
├── Get(name)
├── Set(name, value)
└── Delete(name)
```

Planned implementations:

- Windows: OS-protected credential storage;
- macOS: Keychain integration;
- Linux: Secret Service when available, with an explicit documented fallback for headless deployments.

The existing `.local/commandcode-provider.json` remains a migration/import path until the platform secret layer is implemented and verified.

## Platform boundary

Core collectors, domain models, store, and HTTP API must not import Windows-specific packages.

Platform-specific code is isolated below `internal/platform/<os>` and selected by normal Go build constraints where required.

Target service integrations:

- Windows Service;
- Linux systemd unit generation/install flow;
- macOS launchd plist generation/install flow.

Service installation is a later migration phase. The first Go bootstrap runs as a foreground process.

## Build targets

Initial release matrix:

```text
windows / amd64
linux   / amd64
darwin  / amd64
darwin  / arm64
```

No CGO-dependent database implementation should be introduced unless there is a demonstrated requirement. The SQLite driver choice is deferred until the ZCode collector migration phase so the bootstrap remains standard-library-only.

## Logging

Use `log/slog` with structured, sanitized fields.

Never log:

- API keys;
- Authorization headers;
- raw provider configuration;
- prompt/message bodies;
- full database rows containing private content.

Startup diagnostics may expose adapter type, enabled/disabled state, API bind address, and sanitized source status only.

## Migration rule

Python remains operational on port 8787 until Go has semantic parity.

During shadow mode:

```text
Python reference -> :8787
Go candidate      -> :8788
```

Comparison focuses on semantics rather than exact observation timestamps:

- source health;
- running/waiting/failed/completed counts;
- active Goal title/workspace/activity;
- plan;
- credit values;
- 5h/weekly percentages and reset instants.

Only after parity is demonstrated does Go take port 8787.
