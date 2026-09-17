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

## Secret storage and durable outbox boundaries

On Windows, the service path uses machine-scope DPAPI plus protected ACLs.

On Linux/macOS the SecretStore is explicitly **permission-protected rather than encrypted at rest**: the state directory is forced to `0700`, the secret file to `0600`, and reads reject group/world-accessible state. For root service deployments, run `commandcode configure` with `sudo` so the protected record is root-owned.

The machine config contains the SecretStore path, not the API key.

When a protected Hub credential exists, the remote uploader also requires a writable durable SQLite event outbox. Service adapters set `AI_CONTROL_HUB_OUTBOX` explicitly instead of allowing the root service to fall back to a user-config directory that may be read-only under service hardening:

```text
Linux systemd: /var/lib/ai-control-hud/agent/events.sqlite3
macOS launchd: <machine-config-directory>/events.sqlite3
```

The Linux adapter creates only the dedicated `agent` child directory as root `0700`; it does not loosen permissions on an existing `/var/lib/ai-control-hud` parent. Its hardened unit grants `ReadWritePaths` only to the Agent data child. The macOS LaunchDaemon injects the outbox path through `EnvironmentVariables` and uses the already protected machine-config directory.

Normal service removal preserves the outbox for durable event delivery after reinstall. `remove --purge` deletes the outbox database and its `-wal`/`-shm` sidecars. Binary upgrades never rewrite or delete it.

## Linux systemd

`scripts/ai-control-agent-systemd.sh` manages the Linux adapter. Recommended first install:

```bash
# Operator creates /tmp/commandcode.key first.
sudo ./ai-control-agent commandcode configure \
  --config /etc/ai-control-hud/agent.json \
  --api-key-file /tmp/commandcode.key

bash ./ai-control-agent-systemd.sh install \
  --agent ./ai-control-agent

sudo /usr/local/lib/ai-control-hud/ai-control-agent doctor \
  --config /etc/ai-control-hud/agent.json

rm -f /tmp/commandcode.key
```

Lifecycle and binary upgrade:

```bash
bash ./ai-control-agent-systemd.sh start
bash ./ai-control-agent-systemd.sh status
bash ./ai-control-agent-systemd.sh restart
bash ./ai-control-agent-systemd.sh upgrade --agent ./ai-control-agent
bash ./ai-control-agent-systemd.sh stop
bash ./ai-control-agent-systemd.sh remove
```

`upgrade` is binary-only. The candidate must pass `ai-control-agent version` before service downtime. A running service is stopped, the new binary is atomically swapped in, and the service must remain stably active; startup failure restores the previous binary and restarts it. A stopped service remains stopped. Machine config, CommandCode SecretStore, Hub SecretStore, durable event outbox, systemd unit, and configured source paths are not rewritten.

On first install the adapter resolves the invoking user's standard ZCode paths (`~/.zcode/cli/db/db.sqlite` and `~/.zcode/v2/tasks-index.sqlite`) **before** entering the root `sudo configure` context. Explicit `--runtime-db` / `--task-index-db` values take precedence. If no source can be found, installation fails instead of silently resolving `/root/.zcode`.

The service runs as root from `/usr/local/lib/ai-control-hud/ai-control-agent`. The unit applies `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, read-only home access, and related hardening. `remove` preserves config/SecretStores/outbox for reversible reinstall; `remove --purge` removes the exact Agent machine state, outbox, and installed binary.

## macOS launchd

`scripts/ai-control-agent-launchd.sh` manages the system LaunchDaemon. Its default machine config is:

```text
/Library/Application Support/AI Control HUD/agent.json
```

Recommended first install uses the same explicit key bootstrap:

```bash
CONFIG="/Library/Application Support/AI Control HUD/agent.json"

sudo ./ai-control-agent commandcode configure \
  --config "$CONFIG" \
  --api-key-file /tmp/commandcode.key

bash ./ai-control-agent-launchd.sh install \
  --agent ./ai-control-agent

sudo /usr/local/lib/ai-control-hud/ai-control-agent doctor \
  --config "$CONFIG"

rm -f /tmp/commandcode.key
```

Lifecycle and binary upgrade:

```bash
bash ./ai-control-agent-launchd.sh start
bash ./ai-control-agent-launchd.sh status
bash ./ai-control-agent-launchd.sh restart
bash ./ai-control-agent-launchd.sh upgrade --agent ./ai-control-agent
bash ./ai-control-agent-launchd.sh stop
bash ./ai-control-agent-launchd.sh remove
```

The launchd upgrade uses the same transactional file helper as Linux. Running state means the LaunchDaemon is loaded and reports `state = running`; an unloaded daemon is treated as inactive and remains unloaded after upgrade. A candidate that cannot stay running is booted out, the previous binary is restored, and the previous LaunchDaemon is bootstrapped again.

The adapter resolves caller-home ZCode databases before `sudo`, installs a root LaunchDaemon plist at `/Library/LaunchDaemons/com.aicontrolhud.agent.plist`, and uses the same root-protected SecretStore model. The plist injects the durable outbox path next to the machine config.

The plist is generated with macOS `plutil`, including a real `ProgramArguments` array so paths with spaces are represented as plist values rather than shell-concatenated command strings.

## Unix service validation

Native CI runs service + SecretStore smoke on Ubuntu and macOS arm64. Current validation exercises:

1. create synthetic operator-owned CommandCode and Hub token files;
2. create protected CommandCode and Hub SecretStores under root;
3. install the systemd/launchd service **without** `--provider-config`;
4. verify SecretStore mode `0600`, ZCode path discovery, and `doctor`;
5. delete the plaintext credential files;
6. start/restart the service with protected Hub configuration enabled and require the service-owned durable event outbox to be created;
7. perform a running-state transactional upgrade and require healthy schema-v1 state afterward;
8. upgrade to a fixture that passes `version` but cannot remain a service, require non-zero result, automatic binary rollback, previous service recovery, and healthy schema-v1 state;
9. stop the service, perform an inactive upgrade, and require it to remain inactive/unloaded;
10. require byte-identical machine config, CommandCode SecretStore, and Hub SecretStore across all upgrade/rollback operations;
11. require the durable outbox to survive upgrades, rollback, normal remove, and reinstall;
12. require no `.upgrade.new` or `.upgrade.bak` scratch files after success or successful rollback;
13. remove while preserving machine config/SecretStores/outbox;
14. reinstall with no provider file and no listen override;
15. verify the previous custom listen value and protected credentials are preserved;
16. start/stop again, remove the independent Hub credential, and verify final service purge deletes the outbox database/WAL/SHM.

This proves service reinstall and binary upgrade do not depend on recoverable plaintext credential files and that enabling the remote Hub uploader remains compatible with Unix service hardening.

## Reproducible release pipeline

The unified Go release pipeline builds the Agent for:

- `windows/amd64`;
- `linux/amd64`;
- `darwin/amd64`;
- `darwin/arm64`.

For every Agent target it forces `CGO_ENABLED=0`, builds twice with deterministic Go flags, byte-compares binaries, packages with deterministic metadata, and records SHA-256.

Archive contents are platform-specific:

```text
windows-amd64.zip
  ai-control-agent.exe

linux-amd64.tar.gz
  ai-control-agent
  ai-control-agent-systemd.sh
  ai-control-agent-unix-upgrade.sh

darwin-*.tar.gz
  ai-control-agent
  ai-control-agent-launchd.sh
  ai-control-agent-unix-upgrade.sh
```

The release workflow extracts every archive during PR CI and requires those exact service/upgrade assets with executable mode on Unix. The package builder fixes archive timestamps, ownership, ordering, and permissions so adding the service assets does not weaken reproducibility.

The same release set also includes the standalone Linux/amd64 Go Hub, production Hub bundle, unified checksums, provenance/attestation on eligible release runs, and the machine-readable release manifest.

Artifact attestation is not Windows Authenticode or Apple Developer ID/notarization. Those require external platform identities and must not be claimed unless explicit platform-signing stages are added.

## Support claims

The project distinguishes:

- **cross-built** — binary compiled;
- **native runtime validated** — native CI executed collector/API behavior;
- **service validated** — native CI exercised service adapter + SecretStore/outbox lifecycle, including transactional upgrades on Linux/macOS arm64;
- **target-machine validated** — real deployment hardware completed the field gate.

The current CI establishes the first three levels across the supported matrix, with native service lifecycle validation on Windows, Linux amd64, and macOS arm64. Real Windows + Android core deployment has separately passed the accepted field path documented in the Central Hub roadmap.
