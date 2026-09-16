# G7 Cross-platform Runtime and Release

G7 turns the Go desktop agent into a reproducible, host-validated multi-platform release while keeping the Android schema-v1 contract unchanged.

## Target matrix

| Target | Native CI | Foreground runtime | Service lifecycle | Secret storage |
| --- | --- | --- | --- | --- |
| Windows amd64 | `windows-latest` | validated | Windows SCM validated | DPAPI LocalMachine + protected ACL |
| Linux amd64 | `ubuntu-latest` | validated | systemd adapter, native CI gate | root/user-owned `0600` file in `0700` state directory |
| macOS arm64 | `macos-15` | validated | LaunchDaemon adapter, native CI gate | root/user-owned `0600` file in `0700` state directory |
| macOS amd64 | `macos-15-intel` | validated | same launchd adapter; runtime tested natively | same Unix permission-protected store |

Windows remains the deployment target for the existing Android HUD. Linux/macOS support does not change the phone configuration or API contract.

## Native runtime validation

`Go Agent CI` runs `go vet`, `go test`, native build, and a real foreground runtime smoke on all four target environments. The smoke creates synthetic ZCode Goal/task-index SQLite databases, starts the native binary, and requires:

- `schemaVersion == 1`;
- server version from the Go agent;
- ZCode health `ok`;
- one running synthetic Goal;
- the expected Goal title/activity;
- graceful process termination.

This is intentionally stronger than cross-compilation: a target is not described as runtime-validated until its native runner executes the collector/API path.

## Portable configuration

`ai-control-agent configure` creates a machine/service configuration without putting the CommandCode API key in that JSON file.

Example foreground Unix configuration:

```bash
./ai-control-agent configure \
  --provider-config ~/.local/share/ai-control-hud/commandcode-provider.json \
  --runtime-db ~/.zcode/cli/db/db.sqlite \
  --task-index-db ~/.zcode/v2/tasks-index.sqlite \
  --listen 127.0.0.1:8787

./ai-control-agent run --config ~/.config/ai-control-hud/agent.json
```

On Windows the service path continues to use DPAPI. On Linux/macOS the SecretStore backend is explicitly **permission-protected rather than encrypted at rest**: the state directory is forced to `0700`, the secret file to `0600`, and `Read` rejects group/world-readable secret files. A root service therefore stores the provider record in a root-only file. This distinction is intentional and documented rather than presenting Unix file permissions as cryptographic encryption.

## Linux systemd

`scripts/ai-control-agent-systemd.sh` manages the Linux adapter:

```bash
bash scripts/ai-control-agent-systemd.sh install \
  --agent ./ai-control-agent \
  --provider-config /path/to/commandcode-provider.json \
  --runtime-db "$HOME/.zcode/cli/db/db.sqlite" \
  --task-index-db "$HOME/.zcode/v2/tasks-index.sqlite"

bash scripts/ai-control-agent-systemd.sh start
bash scripts/ai-control-agent-systemd.sh status
bash scripts/ai-control-agent-systemd.sh restart
bash scripts/ai-control-agent-systemd.sh stop
bash scripts/ai-control-agent-systemd.sh remove
```

The installed service runs as root, uses `/usr/local/lib/ai-control-hud/ai-control-agent`, and uses a root-protected config/SecretStore. The unit enables systemd hardening including `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, and read-only home access. `remove` preserves machine state for reversible reinstall; `remove --purge` deletes only the exact config, SecretStore, and installed binary and never recursively deletes an arbitrary config directory.

## macOS launchd

`scripts/ai-control-agent-launchd.sh` manages a system LaunchDaemon:

```bash
bash scripts/ai-control-agent-launchd.sh install \
  --agent ./ai-control-agent \
  --provider-config /path/to/commandcode-provider.json \
  --runtime-db "$HOME/.zcode/cli/db/db.sqlite" \
  --task-index-db "$HOME/.zcode/v2/tasks-index.sqlite"

bash scripts/ai-control-agent-launchd.sh start
bash scripts/ai-control-agent-launchd.sh status
bash scripts/ai-control-agent-launchd.sh restart
bash scripts/ai-control-agent-launchd.sh stop
bash scripts/ai-control-agent-launchd.sh remove
```

The adapter installs a root LaunchDaemon plist at `/Library/LaunchDaemons/com.aicontrolhud.agent.plist`, runs the stable binary under `/usr/local/lib/ai-control-hud`, and uses the permission-protected platform SecretStore. The plist is generated with Python `plistlib` rather than string-concatenated XML, so paths with spaces are encoded correctly.

## Unix service validation

Native CI runs a service + SecretStore smoke on Ubuntu and macOS arm64 when the hosted environment exposes the corresponding service manager. It uses synthetic ZCode data and a non-production provider credential, then exercises:

1. configure/import;
2. service install;
3. SecretStore permission check (`0600`);
4. start and ZCode `ok`;
5. restart and recovery;
6. stop;
7. remove while preserving config/secret;
8. delete the plaintext provider import;
9. reinstall from the preserved SecretStore;
10. start/stop again;
11. final purge.

macOS Intel receives the same launchd code but independently runs the full native foreground collector/API suite.

## Reproducible release pipeline

`Go Agent Release` produces:

- `windows/amd64` ZIP;
- `linux/amd64` tar.gz;
- `darwin/amd64` tar.gz;
- `darwin/arm64` tar.gz.

For every target the workflow:

1. forces `CGO_ENABLED=0`;
2. builds the same target twice with `-trimpath`, `-buildvcs=false`, a cleared Go build ID, and an injected version;
3. byte-compares the two binaries;
4. packages with deterministic timestamps/ownership/mode metadata;
5. writes a per-target SHA-256 file;
6. combines all target hashes into `SHA256SUMS`.

Pull requests execute this release build without publishing. `workflow_dispatch` creates downloadable CI artifacts. A `v*` tag additionally creates a GitHub Release.

## Artifact signing / provenance

G7 uses GitHub artifact attestations (`actions/attest`) for manual/tag release runs. This provides signed build provenance for the release archives and checksum manifest.

This is **not** Windows Authenticode signing and is **not** Apple Developer ID signing/notarization. Those require external platform certificates/identities that are not stored in this repository. Releases must not claim Authenticode/notarization unless those credentials and platform-specific signing stages are explicitly added later.

## Support claims

The project distinguishes these levels:

- **cross-built**: a target binary compiled;
- **native runtime validated**: native CI executed the ZCode + HTTP path;
- **service validated**: native CI exercised the platform service adapter and SecretStore lifecycle;
- **target-machine validated**: the actual deployment machine/device completed the final operational gate.

G7 CI can establish the first three levels. The existing Windows/Android deployment is only considered fully complete after the final target-machine validation requested at the end of the iteration cycle.
