# AI Control Agent (Go)

This directory contains the V2 desktop agent. The Go implementation is the production backend on Windows after G5; Android continues to consume the unchanged schema-v1 API on port 8787.

## Toolchain

- Go 1.27.x
- pure-Go SQLite through `modernc.org/sqlite`
- Windows service / DPAPI integration isolated behind platform packages

## Test

From `agent/`:

```text
go vet ./...
go test ./...
go build ./cmd/ai-control-agent
```

CI runs vet, tests, and build on both Windows and Ubuntu. Windows CI also publishes the executable artifact.

## Foreground production run

The G5 foreground launcher remains available while G6 service integration is being validated:

```powershell
.\scripts\run-go-windows.ps1
```

Direct invocation is also supported:

```powershell
.\.local\bin\ai-control-agent.exe run -listen 0.0.0.0:8787
```

The live endpoints are:

```text
http://127.0.0.1:8787/api/v1/health
http://127.0.0.1:8787/api/v1/state
```

## Windows service

G6 adds machine configuration, a DPAPI SecretStore, and Windows SCM lifecycle commands:

```text
ai-control-agent.exe service install
ai-control-agent.exe service start
ai-control-agent.exe service stop
ai-control-agent.exe service restart
ai-control-agent.exe service status
ai-control-agent.exe service remove
ai-control-agent.exe doctor
```

`service install` snapshots absolute ZCode database paths, imports the existing local CommandCode provider mirror once into DPAPI-protected storage, copies the executable under `Program Files`, and registers an automatic service. Runtime credentials are not read from the project-local plaintext mirror after installation.

See `../docs/G6_WINDOWS_SERVICE.md` for security boundaries, install details, rollback, and the final target-machine validation sequence.

## Fixture mode

For contract tests or isolated API checks:

```text
go run ./cmd/ai-control-agent run -listen 127.0.0.1:8788 -fixture ../server/tests/fixtures/healthy.json
```

Fixture mode does not start live collectors.
