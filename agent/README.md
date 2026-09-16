# AI Control Agent (Go)

This directory contains the V2 desktop-agent migration target.

The Go agent is currently a **shadow/bootstrap implementation**. The Python backend in `../server/` remains the production reference until the migration gates in `../docs/MIGRATION_GO.md` are satisfied.

## Toolchain

- Go 1.27.x
- bootstrap phase uses only the Go standard library

## Test

From this directory:

```text
go vet ./...
go test ./...
go build ./cmd/ai-control-agent
```

## Run with the committed healthy fixture

From the repository root:

```text
go run ./agent/cmd/ai-control-agent run -listen 127.0.0.1:8788 -fixture server/tests/fixtures/healthy.json
```

Then query:

```text
http://127.0.0.1:8788/api/v1/health
http://127.0.0.1:8788/api/v1/state
```

The default listen address is deliberately `127.0.0.1:8788` so the Go candidate cannot collide with the live Python backend on port 8787 during migration.

Running without `-fixture` exposes a valid schema-v1 degraded snapshot with both collectors disabled. Real collectors are implemented in later migration phases.
