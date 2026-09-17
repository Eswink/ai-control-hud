# Central Hub Deployment Runbook — Go Runtime

Status: deployment guide for Central Hub V2 on CentOS/RHEL-family hosts.

The production Hub runtime is a standalone Go binary named `ai-control-hub`. The CentOS host does **not** need Python, pip, virtualenv, or a Go compiler. CI produces a statically linked Linux/amd64 binary and packages it with the systemd installer and deployment docs.

The former Python/FastAPI Central Hub was retired after the core Go production path passed real-device acceptance. It is no longer a supported runtime or backup path in this repository.

## 1. Architecture and security model

```text
Windows development machine        trusted LAN             CentOS Hub                Android HUD
ZCode / CommandCode -> Go Agent  -------------------->  Go ai-control-hub  <-------  native Android
                                  HTTP ingest               SQLite                     state/events/TTS
                                  UDP discovery 8788
```

- Windows remains the only machine that reads ZCode and CommandCode sources.
- Windows initiates outbound Hub uploads; the Hub never polls Windows.
- Vendor credentials never leave Windows.
- CommandCode API keys are supplied manually by the operator and are never auto-retrieved by the project.
- Agent ingest uses a separate randomly generated bearer token.
- Android never receives the ingest token.
- Automatic LAN discovery advertises only service metadata; no credential or HUD state is included.
- Public Internet exposure is outside this deployment model.

## 2. Files installed on CentOS

```text
/usr/local/lib/ai-control-hub/ai-control-hub   root-owned standalone Go binary
/etc/ai-control-hud/hub.env                   root:root mode 0600; token/core config
/var/lib/ai-control-hud/hub.sqlite3           persistent SQLite database
/etc/systemd/system/ai-control-hub.service    hardened systemd unit
```

The process runs as the unprivileged `ai-control-hub` user. The unit keeps `ProtectSystem=strict`, `ProtectHome=true`, empty capabilities, `NoNewPrivileges=true`, and grants write access only to `/var/lib/ai-control-hud`.

## 3. Network modes

### Preferred: automatic trusted-LAN mode

```bash
--lan-auto
```

This explicitly enables:

- HTTP on `0.0.0.0:8787`;
- UDP discovery on `0.0.0.0:8788`;
- clients using stable identities `auto://lan` and `http://auto.lan`.

Ensure the trusted LAN firewall permits **TCP 8787** and **UDP 8788**. A missing UDP 8788 rule makes both Windows and Android auto-discovery fail even while the Hub itself is healthy.

The physical DHCP address may currently be `192.168.101.103`, but that address must not be stored as the logical auto-mode identity.

### Manual fallback

For routed VLANs, AP client isolation, or a network that blocks broadcast:

```bash
--listen <FIXED_PRIVATE_IP>:8787
```

### Safe default

Without `--lan-auto` or `--listen`, the installer renders loopback-only `127.0.0.1:8787`.

## 4. Obtain the deployment bundle

Use the CI artifact named:

```text
ai-control-hub-h3-validation-bundle
```

The historical artifact name still contains `H3`; the bundle itself is the normal Go Hub deployment bundle. It contains the prebuilt Linux binary, installer, docs, build metadata, and checksums.

Verify before installation:

```bash
sha256sum -c SHA256SUMS
chmod +x ./ai-control-hub
./ai-control-hub version
```

If the executable cannot run, use the correct architecture artifact rather than installing a compiler or changing the host Python version.

## 5. Generate the Hub ingestion token

Generate a new project-internal token. This is **not** a CommandCode or ZCode credential.

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Transfer the same temporary file to Windows through a trusted channel. Never paste it into GitHub, chat, screenshots, or command-line literals.

## 6. Install the Go Hub

Inspect the auto-LAN unit first:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit \
  --lan-auto \
  --hub-id dorm-hub
```

Confirm `ExecStart` is:

```text
/usr/local/lib/ai-control-hub/ai-control-hub serve --host 0.0.0.0 --port 8787
```

Install:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --binary "$PWD/ai-control-hub" \
  --token-file "$HOME/ai-control-hub.token" \
  --lan-auto \
  --hub-id dorm-hub \
  --agent-id desktop-main
```

Installation enables the service for boot but intentionally does not start it immediately. Review:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
```

Expected:

- `hub.env` mode `0600`, owner `root:root`;
- persistent data directory owned by `ai-control-hub`;
- no bearer token inside the unit;
- no Python/uvicorn/venv runtime in `ExecStart`.

Start:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

Check direct HTTP through the currently observed LAN IP:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/health
```

Before the Windows Agent uploads its first snapshot, `/api/v1/health` is expected to report source errors and `/api/v1/state` is expected to return HTTP 503. That means “Hub is running but has no agent snapshot yet,” not “Hub process is down.”

## 7. Migration from an older Python Hub installation

The Go implementation preserves the original Central Hub SQLite tables:

```text
agents
snapshots
events
idx_events_agent_seq
```

Default database path remains:

```text
/var/lib/ai-control-hud/hub.sqlite3
```

Therefore an existing database from an older Python deployment can be reused in place. The Go installer:

1. stops the current `ai-control-hub.service`;
2. preserves `/var/lib/ai-control-hud`;
3. installs the Go binary;
4. removes only the legacy `/usr/local/lib/ai-control-hub/venv` if present;
5. replaces the unit with the Go `ExecStart`;
6. leaves the existing database intact.

The repository no longer ships the old Python Hub runtime. Rollback should use a previously retained deployment artifact/database backup rather than recreating the retired Python service from current source.

## 8. Configure Windows auto-discovery

In elevated PowerShell:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-auto `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

Expected status includes the logical URL and current physical resolution:

```text
url=auto://lan resolved=http://192.168.101.103:8787 hub=dorm-hub
```

Then check whether the Windows SCM service already exists:

```powershell
.\ai-control-agent.exe service status
```

If it reports `installed=false`, perform the first-time installation described in `docs/H3_WINDOWS_FIRST_INSTALL.md`. The CommandCode key must be entered/provided by the operator; do not attempt to auto-retrieve it.

After installation:

```powershell
.\ai-control-agent.exe doctor --live
.\ai-control-agent.exe service start
```

If the service is already installed, restart it instead:

```powershell
.\ai-control-agent.exe service restart
```

Verify:

```powershell
.\ai-control-agent.exe service status
curl.exe http://127.0.0.1:8787/api/v1/state
```

Then on CentOS verify `/api/v1/state` changes from the expected pre-upload 503 to HTTP 200 with the uploaded schema-v1 snapshot.

After the end-to-end path is proven, delete temporary plaintext Hub-token/provider-import files. Keep the DPAPI SecretStore records and `/etc/ai-control-hud/hub.env`.

## 9. Android

Fresh installations use automatic discovery by default. Existing installations can open `SERVER`, enter:

```text
auto.lan
```

and reconnect.

The dashboard displays the resolved address:

```text
AUTO · http://<CURRENT_HUB_IP>:8787
```

The stable event cursor identity remains `http://auto.lan`, so a DHCP IP change does not look like a different Hub.

## 10. Logs and status

```bash
bash scripts/ai-control-hub-systemd.sh status
sudo journalctl -u ai-control-hub.service --since today
/usr/local/lib/ai-control-hub/ai-control-hub version
```

Do not include `hub.env`, bearer tokens, CommandCode keys, DPAPI blobs, or complete process environments in diagnostics.

## 11. Online backup — Go only

Create a protected directory once:

```bash
sudo install -d -o ai-control-hub -g ai-control-hub -m 0750 /var/lib/ai-control-hud/backups
```

Run a live backup:

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/ai-control-hub backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/hub-${stamp}.sqlite3"
```

The command:

- refuses source=destination;
- refuses overwrite;
- uses SQLite `VACUUM INTO` to create a consistent database image while the Hub is live;
- runs `PRAGMA integrity_check` on the result;
- sets mode `0600`;
- fsyncs and atomically publishes the file.

Success prints `integrity=ok`.

There is no Python backup command in the supported path.

## 12. Restore

Restore is a maintenance operation:

```bash
bash scripts/ai-control-hub-systemd.sh stop
sudo rm -f /var/lib/ai-control-hud/hub.sqlite3-wal /var/lib/ai-control-hud/hub.sqlite3-shm
sudo install -o ai-control-hub -g ai-control-hub -m 0600 \
  /path/to/validated-backup.sqlite3 \
  /var/lib/ai-control-hud/hub.sqlite3
bash scripts/ai-control-hub-systemd.sh start
```

Then validate `/health`, `/state`, `/events`, Windows upload, and Android cursor behavior. A restore to a lower event high-water mark is handled by the existing Android silent-rebase logic.

## 13. Token rotation

Generate a new temporary token file, then on CentOS:

```bash
bash scripts/ai-control-hub-systemd.sh rotate-token \
  --token-file "$HOME/ai-control-hub-next.token"
```

On Windows:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-token-file C:\Temp\ai-control-hub-next.token
.\ai-control-agent.exe service restart
```

The Agent durable outbox tolerates the brief 401 transition. Delete both temporary plaintext token files after confirming fresh state/event delivery.

## 14. Optional operational drills

The core real-device cutover was accepted on 2026-09-17. The operator explicitly chose not to block further development on the remaining disaster-recovery drills below. They remain useful operational exercises but are recorded as `NOT RUN`, not product failures:

- Hub/network outage with durable-event catch-up;
- CentOS reboot/autostart persistence;
- DHCP address change and rediscovery;
- live backup/restore drill.

The implementation and CI coverage for these behaviors remain in place.
