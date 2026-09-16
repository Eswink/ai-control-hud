# Go Migration Plan

Status: active.

Goal: replace the Python long-running desktop backend with a cross-platform Go agent without changing the Android schema-v1 contract.

## Non-goals

The migration does **not** initially:

- redesign the Android UI;
- change `/api/v1/state` or `/api/v1/health` semantics;
- introduce schema version 2;
- delete the Python implementation;
- install an OS service before foreground parity is proven;
- move credentials into a new secret store before the Go collector path is functionally equivalent.

## Baseline that must remain reproducible

The verified Python implementation is the migration oracle. On the target Windows machine it has demonstrated:

- live ZCode Goal detection;
- heartbeat-based active-session filtering;
- current todo/activity rendering;
- CommandCode plan `GOAT`;
- remaining credit and verified plan ceiling;
- 5-hour usage/reset;
- weekly usage/reset;
- `overall.status = live` when both sources are healthy;
- stale/error semantics that preserve last-known-good data only after a prior success.

The migration must preserve these semantics before Python can be retired.

## Phase 0 — Freeze executable contract

Deliverables:

- keep `docs/API.md` as the canonical external contract;
- preserve Python schema-v1 fixtures as migration inputs;
- add Go domain types matching schema v1;
- add Go golden tests that decode/encode the committed fixtures;
- document normalization differences that are timestamp-only or ordering-only.

Exit gate:

- Go tests prove every committed schema-v1 fixture can be decoded and re-encoded without losing required meaning;
- no vendor-specific field is added to the API contract.

## Phase 1 — Go agent bootstrap

Deliverables:

- `agent/go.mod`;
- `cmd/ai-control-agent/main.go`;
- domain models;
- in-memory snapshot store;
- `GET /api/v1/state`;
- `GET /api/v1/health`;
- mock fixture loading;
- graceful shutdown using signal-aware context;
- configurable listen address, defaulting to `127.0.0.1:8788` during migration.

Use the Go standard library only in this phase where practical.

Exit gate:

- `go test ./...` passes;
- mock HTTP responses conform to schema v1;
- Android can point to the Go mock agent without needing an APK change.

## Phase 2 — ZCode collector migration

Deliverables:

- read-only SQLite access to the verified runtime DB;
- runtime schema compatibility checks;
- live Goal mapping from `session`, `session_target`, and `todo`;
- heartbeat freshness policy;
- privacy-preserving stable IDs;
- bounded recent task-index fallback;
- isolated collector loop and last-known-good semantics.

Driver requirement:

- prefer a pure-Go SQLite driver to keep cross-compilation simple;
- do not add CGO unless a verified blocker requires it.

Exit gate:

- on Windows, Go and Python identify the same live Goal/activity;
- stale historical sessions are not reported as running;
- idle behavior matches the current Python implementation;
- DB failures become sanitized `error/stale`, never an empty-but-healthy source.

## Phase 3 — CommandCode collector migration

Deliverables:

- use the verified provider credential path during migration;
- isolate unstable billing endpoints inside the collector package;
- normalize plan/credit/5h/weekly data into schema v1;
- preserve partial-data rules;
- bounded HTTP timeout;
- independent 60-second refresh cadence;
- secret-safe errors and logs.

Exit gate:

- target account returns the same plan and materially identical credit/window values as Python at comparable observation times;
- 401/403 is distinguishable from zero usage;
- temporary failure produces stale last-known-good after prior success.

## Phase 4 — Shadow mode

Run both implementations simultaneously:

```text
Python reference :8787
Go candidate     :8788
```

Compare snapshots for a meaningful operating window.

Fields that must match semantically:

- source health;
- ZCode summary counts;
- active task title/workspace/status/activity;
- plan;
- credit remaining/limit/unit;
- usage window names/percentages/reset times.

Expected allowed differences:

- server uptime;
- server observation timestamps;
- sub-second collection timing;
- JSON field ordering.

Exit gate:

- no unexplained semantic drift during normal work, idle periods, backend restart, ZCode Goal transition, and one simulated vendor failure.

## Phase 5 — Cut over Windows

Steps:

1. stop Python backend;
2. start Go agent on `0.0.0.0:8787`;
3. leave Android configuration unchanged;
4. run device smoke test;
5. retain documented one-command Python rollback until the next phase is complete.

Exit gate:

- target Android device remains LIVE on the Go backend;
- restart/reconnect behavior is acceptable;
- no Python process is required for normal operation.

## Phase 6 — Native desktop service and secret storage

Windows first:

- foreground `run` command;
- `doctor` command;
- service install/start/stop/remove commands;
- OS-protected CommandCode secret storage;
- migration/import helper for the existing gitignored local provider mirror.

Then implement equivalent platform adapters for Linux/macOS.

Exit gate:

- Windows can boot into the agent without an interactive shell;
- credentials are not stored in the repository or Android app;
- service removal and foreground fallback are documented.

## Phase 7 — Cross-platform release pipeline

Target binaries:

```text
ai-control-agent-windows-amd64.exe
ai-control-agent-linux-amd64
ai-control-agent-darwin-amd64
ai-control-agent-darwin-arm64
```

Add CI tests on host-native runners first; cross-build artifacts only after tests are green.

Do not claim Linux/macOS runtime support until source discovery/path behavior has been tested on those operating systems. Cross-compilation is not equivalent to runtime validation.

## Rollback policy

Until Windows cutover is stable:

- Python remains in `server/`;
- existing Windows launcher remains usable;
- Android stays on schema v1;
- Go migration must not mutate ZCode databases;
- switching ports is enough to move between reference and candidate implementations.

If Go collector behavior is uncertain, fail visibly or keep the Python reference active. Do not silently reinterpret unknown vendor state.

## Issue sequence

Recommended dependency chain:

```text
G0 contract + golden fixtures
        │
        ▼
G1 Go mock agent/API
        │
        ├───────────────┐
        ▼               ▼
G2 ZCode collector   G3 CommandCode collector
        │               │
        └───────┬───────┘
                ▼
           G4 shadow parity
                ▼
           G5 Windows cutover
                ▼
      G6 service + secret store
                ▼
       G7 cross-platform release
```

## Current immediate execution target

The active implementation task is Phase 0 + Phase 1:

- create the Go module using the current supported Go toolchain;
- implement schema-v1 domain structs;
- implement an in-memory snapshot store;
- implement mock `/api/v1/state` and `/api/v1/health`;
- add Go tests against committed JSON fixtures;
- keep the default Go listen port at `8788` to avoid colliding with the live Python service.
