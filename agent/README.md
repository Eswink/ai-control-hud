# AI Control Agent (Go)

This directory contains the production development-machine Agent. It collects ZCode/CommandCode locally, exposes bounded local diagnostics, records durable task events, and pushes normalized state/events outbound to the 24/7 Go Central Hub.

Vendor credentials remain on the development machine. The Hub never polls the Agent.

## Toolchain

- Go 1.27.x
- pure-Go SQLite through `modernc.org/sqlite`
- Windows service / DPAPI integration isolated behind platform packages
- Linux systemd and macOS launchd adapters for cross-platform development/CI

## Test

From `agent/`:

```text
go vet ./...
go test ./...
go build ./cmd/ai-control-agent
```

CI runs vet, tests, native runtime smoke, and builds on Windows, Linux, macOS arm64, and macOS amd64. Windows CI also publishes the executable artifact.

## Local API

The Agent keeps a local/trusted-LAN HTTP API even when Hub upload is unavailable:

```text
GET http://127.0.0.1:8787/api/v1/health
GET http://127.0.0.1:8787/api/v1/state
GET http://127.0.0.1:8787/api/v1/diagnostics
```

`/api/v1/state` is the canonical schema-v1 snapshot used by the Hub uploader.

`/api/v1/health` is the small source-health envelope.

`/api/v1/diagnostics` is an independently versioned operational endpoint. It reports fixed adapter kinds, enabled/status state, observed/last-success ages, and schema-support classification. It deliberately excludes database paths, provider URLs, task/workspace data, raw source errors, tokens, API keys, and SecretStore contents. See `../docs/AGENT_DIAGNOSTICS.md`.

## Foreground troubleshooting

The foreground launcher remains available:

```powershell
.\scripts\run-go-windows.ps1
```

Direct invocation is also supported:

```powershell
.\.local\bin\ai-control-agent.exe run -listen 0.0.0.0:8787
```

Production Windows deployments should normally use the SCM service instead of keeping an interactive foreground process running.

## Central Hub upload

Production Hub configuration is stored in the platform SecretStore. On Windows, `hub configure` imports the operator-created Hub token file into machine-scope DPAPI and stores the stable `auto://lan` identity rather than a DHCP address:

```powershell
ai-control-agent.exe hub configure `
  --hub-auto `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

ai-control-agent.exe hub status
```

When enabled, the Agent:

- keeps the local API running;
- uploads the current canonical snapshot to `/api/v1/agent/state` every 5 seconds;
- sends an independent heartbeat every 10 seconds;
- observes terminal task transitions independently from network delivery;
- stores durable events in a local SQLite outbox before upload;
- retries with bounded timeout/backoff;
- re-discovers the Hub after transport/address failure in auto mode;
- never logs the Hub bearer token or vendor credentials.

Loss of the Hub/network does not stop local collection or local task-event capture.

Environment-variable Hub configuration remains available for development/CI fallback, but protected platform storage is the production path.

## Windows service

Windows production lifecycle commands:

```text
ai-control-agent.exe service install
ai-control-agent.exe service start
ai-control-agent.exe service stop
ai-control-agent.exe service restart
ai-control-agent.exe service status
ai-control-agent.exe service remove
ai-control-agent.exe doctor
```

`service install` resolves absolute ZCode database paths, imports an operator-supplied CommandCode provider credential into DPAPI when needed, copies the executable under `Program Files`, creates Private/Domain firewall rules, and registers the `AIControlHUD` automatic service.

CommandCode API keys are manual/operator-supplied only. The project does not scrape, discover, recover, or auto-retrieve a real CommandCode key. After successful DPAPI import, the runtime does not need the plaintext provider import file.

See `../docs/G6_WINDOWS_SERVICE.md` and `../docs/H3_WINDOWS_FIRST_INSTALL.md` for install/security details.

## Fixture mode

For contract tests or isolated API checks:

```text
go run ./cmd/ai-control-agent run -listen 127.0.0.1:8788 -fixture ../server/tests/fixtures/healthy.json
```

Fixture mode does not start live collectors. If Hub configuration is present, fixture state can also be uploaded for isolated integration testing.
