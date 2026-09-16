# AI Control Agent (Go)

This directory contains the V2 desktop agent. The Go implementation is the production backend on Windows after G5. During the Central Hub V2 migration the agent keeps its local schema-v1 HTTP API and can additionally push the same canonical state to a 24/7 hub.

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

CI runs vet, tests, and build on Windows, Linux, and macOS. Windows CI also publishes the executable artifact.

## Foreground production run

The G5 foreground launcher remains available:

```powershell
.\scripts\run-go-windows.ps1
```

Direct invocation is also supported:

```powershell
.\.local\bin\ai-control-agent.exe run -listen 0.0.0.0:8787
```

The local diagnostic endpoints remain:

```text
http://127.0.0.1:8787/api/v1/health
http://127.0.0.1:8787/api/v1/state
```

## Central Hub upload

Remote upload is disabled unless hub configuration is present. For the current H2 development slice the uploader is configured through environment variables:

```text
AI_CONTROL_HUB_URL=https://your-private-hub.example
AI_CONTROL_HUB_AGENT_ID=desktop-main
AI_CONTROL_HUB_TOKEN=<agent bearer token>
```

When enabled, the same process:

- keeps the existing local API running;
- POSTs the current canonical schema-v1 state to `/api/v1/agent/state` every 5 seconds;
- POSTs an independent heartbeat to `/api/v1/agent/heartbeat` every 10 seconds;
- uses a 4-second request timeout;
- applies bounded exponential retry backoff up to 30 seconds;
- never logs the bearer token or server response body.

The hub uploader is deliberately non-fatal after startup: loss of the server/network does not stop local collection or the local HTTP API.

For a trusted LAN or encrypted private overlay, `http://` is technically accepted by the development client. Prefer HTTPS when traffic is not already protected by the network layer because the bearer token is an authentication credential.

The environment-variable token is an interim H2 mechanism. Production Windows-service installation will move the hub credential into protected machine storage before the deployment-hardening milestone is considered complete.

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

`service install` snapshots absolute ZCode database paths, imports the existing local CommandCode provider mirror once into DPAPI-protected storage, copies the executable under `Program Files`, and registers an automatic service. Runtime CommandCode credentials are not read from the project-local plaintext mirror after installation.

See `../docs/G6_WINDOWS_SERVICE.md` for security boundaries, install details, rollback, and the final target-machine validation sequence.

## Fixture mode

For contract tests or isolated API checks:

```text
go run ./cmd/ai-control-agent run -listen 127.0.0.1:8788 -fixture ../server/tests/fixtures/healthy.json
```

Fixture mode does not start live collectors. If Central Hub environment variables are present, fixture state is also uploaded, which is useful for end-to-end hub testing without a live ZCode installation.
