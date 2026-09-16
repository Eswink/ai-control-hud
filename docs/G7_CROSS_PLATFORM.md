# G7 Cross-platform Runtime and Release

G7 provides a host-validated multi-platform Go Agent while keeping the Android schema-v1 contract unchanged.

## Target matrix

| Target | Native CI | Foreground runtime | Service lifecycle | Secret storage |
| --- | --- | --- | --- | --- |
| Windows amd64 | `windows-latest` | validated | Windows SCM validated | DPAPI LocalMachine + protected ACL |
| Linux amd64 | `ubuntu-latest` | validated | systemd validated | root/user-owned `0600` file in `0700` state directory |
| macOS arm64 | `macos-15` | validated | LaunchDaemon validated | root/user-owned `0600` file in `0700` state directory |
| macOS amd64 | `macos-15-intel` | validated | adapter source shared; runtime tested natively | same Unix permission-protected store |

Windows remains the real-device deployment target for the current Android HUD. Linux/macOS support does not change the phone API contract.

## Native runtime validation

`Go Agent CI` runs `go vet`, `go test`, native build, and a foreground runtime smoke on all four targets. The smoke creates synthetic ZCode Goal/task-index databases, starts the native binary, and requires schema-v1 state, healthy synthetic ZCode data, diagnostics, and graceful termination.

This is stronger than cross-compilation: a target is not described as runtime-validated until its native runner executes the collector/API path.

## CommandCode credential rule

CommandCode API keys are supplied explicitly by the operator. The project does not discover, scrape, recover, or auto-retrieve a real key from ZCode, accounts, files, applications, or external services.

The preferred bootstrap on every supported desktop OS is a temporary one-line file supplied by the operator:

```bash
sudo ./ai-control-agent commandcode configure \
  --config /etc/ai-control-hud/agent.json \
  --api-key-file /tmp/commandcode.key

sudo ./ai-control-agent commandcode status \
  --config /etc/ai-control-hud/agent.json
```

After service installation and `doctor` validation, delete the temporary plaintext file:

```bash
rm -f /tmp/commandcode.key
```

The key itself is never accepted as a command-line literal and is never printed by the Agent.

The older `--provider-config PATH` flow remains available only as an **explicit** compatibility/migration input. Machine/service configuration no longer probes `.local/commandcode-provider.json` or `HUD_ZCODE_CONFIG` implicitly.

## Secret storage boundaries

On Windows, the service path uses machine-scope DPAPI plus protected ACLs.

On Linux/macOS the SecretStore is explicitly **permission-protected rather than encrypted at rest**: the state directory is forced to `0700`, the secret file to `0600`, and reads reject group/world-accessible state. For root service deployments, run `commandcode configure` with `sudo` so the protected record is root-owned.

The machine config contains the SecretStore path, not the API key.

## Linux systemd

`scripts/ai-control-agent-systemd.sh` manages the Linux adapter. Recommended first install:

```bash
# Operator creates /tmp/commandcode.key first.
sudo ./ai-control-agent commandcode configure \
  --config /etc/ai-control-hud/agent.json \
  --api-key-file /tmp/commandcode.key

bash scripts/ai-control-agent-systemd.sh install \
  --agent ./ai-control-agent

sudo /usr/local/lib/ai-control-hud/ai-control-agent doctor \
  --config /etc/ai-control-hud/agent.json

rm -f /tmp/commandcode.key
```

Then use:

```bash
bash scripts/ai-control-agent-systemd.sh start
bash scripts/ai-control-agent-systemd.sh status
bash scripts/ai-control-agent-systemd.sh restart
bash scripts/ai-control-agent-systemd.sh stop
bash scripts/ai-control-agent-systemd.sh remove
```

On first install the adapter resolves the invoking user's standard ZCode paths (`~/.zcode/cli/db/db.sqlite` and `~/.zcode/v2/tasks-index.sqlite`) **before** entering the root `sudo configure` context. Explicit `--runtime-db` / `--task-index-db` values take precedence. If no source can be found, installation fails instead of silently resolving `/root/.zcode`.

The service runs as root from `/usr/local/lib/ai-control-hud/ai-control-agent`. The unit applies `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, read-only home access, and related hardening. `remove` preserves config/SecretStore for reversible reinstall; `remove --purge` removes the exact machine state and installed binary.

## macOS launchd

`scripts/ai-control-agent-launchd.sh` manages the system LaunchDaemon. Recommended first install uses the same explicit key bootstrap:

```bash
sudo ./ai-control-agent commandcode configure \
  --config /etc/ai-control-hud/agent.json \
  --api-key-file /tmp/commandcode.key

bash scripts/ai-control-agent-launchd.sh install \
  --agent ./ai-control-agent

sudo /usr/local/lib/ai-control-hud/ai-control-agent doctor \
  --config /etc/ai-control-hud/agent.json

rm -f /tmp/commandcode.key
```

The adapter resolves caller-home ZCode databases before `sudo`, installs a root LaunchDaemon plist at `/Library/LaunchDaemons/com.aicontrolhud.agent.plist`, and uses the same root-protected SecretStore model.

The plist is generated with macOS `plutil`, including a real `ProgramArguments` array so paths with spaces are represented as plist values rather than shell-concatenated command strings.

## Unix service validation

Native CI runs service + SecretStore smoke on Ubuntu and macOS arm64. Current validation exercises:

1. create a synthetic operator-owned one-line key file;
2. `commandcode configure --api-key-file` under root and verify `commandcode status`;
3. install the systemd/launchd service **without** `--provider-config`;
4. verify SecretStore mode `0600` and ZCode path discovery;
5. run `doctor`;
6. delete the plaintext key file;
7. start/restart/stop the service;
8. remove while preserving machine config/SecretStore;
9. reinstall with no provider file and no listen override;
10. verify the previous custom listen value and protected credential are preserved;
11. start/stop again and final purge.

This proves service reinstall does not depend on a recoverable plaintext provider/key file.

## Reproducible release pipeline

The unified Go release pipeline builds the Agent for:

- `windows/amd64`;
- `linux/amd64`;
- `darwin/amd64`;
- `darwin/arm64`.

For every Agent target it forces `CGO_ENABLED=0`, builds twice with deterministic Go flags, byte-compares binaries, packages with deterministic metadata, and records SHA-256. The same release set also includes the standalone Linux/amd64 Go Hub, production Hub bundle, unified checksums, provenance/attestation on eligible release runs, and the machine-readable release manifest introduced later in the Hub V2 stack.

Artifact attestation is not Windows Authenticode or Apple Developer ID/notarization. Those require external platform identities and must not be claimed unless explicit platform-signing stages are added.

## Support claims

The project distinguishes:

- **cross-built** — binary compiled;
- **native runtime validated** — native CI executed collector/API behavior;
- **service validated** — native CI exercised service adapter + SecretStore lifecycle;
- **target-machine validated** — real deployment hardware completed the field gate.

The current CI establishes the first three levels across the supported matrix. Real Windows + Android core deployment has separately passed the accepted field path documented in the Central Hub roadmap.
