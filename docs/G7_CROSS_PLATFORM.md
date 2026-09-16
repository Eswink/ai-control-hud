# G7 Cross-platform Runtime and Release

G7 turns the Go desktop agent into a reproducible, host-validated multi-platform release while keeping the Android schema-v1 contract unchanged.

## Target matrix

| Target | Native CI | Foreground runtime | Service lifecycle | Secret storage |
| --- | --- | --- | --- | --- |
| Windows amd64 | `windows-latest` | validated | Windows SCM validated | DPAPI LocalMachine + protected ACL |
| Linux amd64 | `ubuntu-latest` | validated | systemd validated | root/user-owned `0600` file in `0700` state directory |
| macOS arm64 | `macos-15` | validated | LaunchDaemon validated | root/user-owned `0600` file in `0700` state directory |
| macOS amd64 | `macos-15-intel` | validated | same launchd adapter; runtime tested natively | same Unix permission-protected store |

Windows remains the deployment target for the existing Android HUD. Linux/macOS support does not change the phone configuration or API contract.

## Native runtime validation

`Go Agent CI` runs `go vet`, `go test`, native build, and a real foreground runtime smoke on all four target environments. The smoke creates synthetic ZCode Goal/task-index SQLite databases, starts the native binary, and requires:

- `schemaVersion == 1`;
- server version from the Go agent;
- ZCode health `ok`;
- one running synthetic Goal;
- the expected Goal title/activity;
- graceful process termination, including SIGTERM on Unix.

This is intentionally stronger than cross-compilation: a target is not described as runtime-validated until its native runner executes the collector/API path.

## Portable configuration

`ai-control-agent configure` creates a machine/service configuration without putting the CommandCode API key in that JSON file. `ai-control-agent config get` exposes only selected non-secret configuration fields for platform lifecycle scripts; it never prints the API key.

Example foreground Unix configuration:

```bash
./ai-control-agent configure \
  --provider-config ~/.local/share/ai-control-hud/commandcode-provider.json \
  --runtime-db ~/.zcode/cli/db/db.sqlite \
  --task-index-db ~/.zcode/v2/tasks-index.sqlite \
  --listen 127.0.0.1:8787

./ai-control-agent run --config ~/.config/ai-control-hud/agent.json
```

On Windows the service path continues to use DPAPI. On Linux/macOS the SecretStore backend is explicitly **permission-protected rather than encrypted at rest**: the state directory is forced to `0700`, the secret file to `0600`, and `Read` rejects both a group/world-accessible secret file and a group/world-accessible parent state directory. A root service therefore stores the provider record in a root-only file. This distinction is intentional and documented rather than presenting Unix file permissions as cryptographic encryption.

## Linux systemd

`scripts/ai-control-agent-systemd.sh` manages the Linux adapter:

```bash
bash scripts/ai-control-agent-systemd.sh install \
  --agent ./ai-control-agent \
  --provider-config /path/to/commandcode-provider.json

bash scripts/ai-control-agent-systemd.sh start
bash scripts/ai-control-agent-systemd.sh status
bash scripts/ai-control-agent-systemd.sh restart
bash scripts/ai-control-agent-systemd.sh stop
bash scripts/ai-control-agent-systemd.sh remove
```

On first install the adapter resolves the invoking user's standard ZCode paths (`~/.zcode/cli/db/db.sqlite` and `~/.zcode/v2/tasks-index.sqlite`) **before** entering the root `sudo configure` context. Explicit `--runtime-db` / `--task-index-db` values take precedence. If no source can be found, installation fails with an explicit request for one of those flags instead of silently resolving `/root/.zcode`.

The installed service runs as root, uses `/usr/local/lib/ai-control-hud/ai-control-agent`, and uses a root-protected config/SecretStore. The unit enables systemd hardening including `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=strict`, and read-only home access. `remove` preserves machine state for reversible reinstall; `remove --purge` deletes only the exact config, SecretStore, and installed binary and never recursively deletes an arbitrary config directory. Custom config paths are converted to absolute paths before being persisted into the service definition.

## macOS launchd

`scripts/ai-control-agent-launchd.sh` manages a system LaunchDaemon:

```bash
bash scripts/ai-control-agent-launchd.sh install \
  --agent ./ai-control-agent \
  --provider-config /path/to/commandcode-provider.json

bash scripts/ai-control-agent-launchd.sh start
bash scripts/ai-control-agent-launchd.sh status
bash scripts/ai-control-agent-launchd.sh restart
bash scripts/ai-control-agent-launchd.sh stop
bash scripts/ai-control-agent-launchd.sh remove
```

Like the Linux adapter, first install resolves the invoking user's standard `.zcode` databases before `sudo`; explicit source flags override discovery. The adapter installs a root LaunchDaemon plist at `/Library/LaunchDaemons/com.aicontrolhud.agent.plist`, runs the stable binary under `/usr/local/lib/ai-control-hud`, and uses the permission-protected platform SecretStore.

The production adapter has no Python runtime dependency. Its plist is generated with macOS `plutil`, including a real `ProgramArguments` array, so paths with spaces are represented as plist values rather than shell-concatenated command strings.

## Unix service validation

Native CI runs a service + SecretStore smoke on Ubuntu and macOS arm64. It uses synthetic ZCode data and a non-production provider credential, then exercises:

1. configure/import;
2. service install;
3. SecretStore permission check (`0600`);
4. start and ZCode `ok`;
5. restart and recovery;
6. stop;
7. remove while preserving config/secret;
8. delete the plaintext provider import;
9. reinstall from the preserved SecretStore **without** a listen override;
10. verify the previous custom listen value is preserved;
11. start/stop again;
12. final purge.

The Linux systemd flow and macOS arm64 LaunchDaemon flow have both been exercised successfully on native GitHub-hosted runners. macOS Intel independently runs the full native foreground collector/API suite against the same Go packages and launchd adapter source.

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

Workflow permissions are separated by responsibility: build/manifest jobs are read-only; the provenance job alone receives OIDC/attestation/artifact-metadata write permissions; the tag publishing job alone receives repository contents write permission.

## Artifact signing / provenance

G7 uses GitHub artifact attestations (`actions/attest`) for manual/tag release runs. This provides signed Sigstore build provenance for the release archives and checksum manifest.

This is **not** Windows Authenticode signing and is **not** Apple Developer ID signing/notarization. Those require external platform certificates/identities that are not stored in this repository. Releases must not claim Authenticode/notarization unless those credentials and platform-specific signing stages are explicitly added later.

## Support claims

The project distinguishes these levels:

- **cross-built**: a target binary compiled;
- **native runtime validated**: native CI executed the ZCode + HTTP path;
- **service validated**: native CI exercised the platform service adapter and SecretStore lifecycle;
- **target-machine validated**: the actual deployment machine/device completed the final operational gate.

G7 CI establishes the first three levels for the supported matrix above. The existing Windows/Android deployment is only considered fully complete after the final target-machine validation requested at the end of the iteration cycle.
