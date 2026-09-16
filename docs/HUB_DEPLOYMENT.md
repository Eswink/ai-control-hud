# Central Hub Deployment Runbook

Status: deployment guide for Central Hub V2 (H3/H6)

This guide deploys the AI Control Hub to a 24/7 CentOS/RHEL-family host, keeps ZCode/CommandCode collection on the Windows development machine, and points the Android HUD at the Hub over a private network.

The intended transport is a trusted LAN or a private overlay such as Tailscale/WireGuard. Public Internet exposure is not part of this design.

## 1. Security model

- Windows is the only place that reads ZCode and CommandCode sources.
- Windows initiates outbound Hub requests; the Hub never polls Windows.
- CommandCode credentials never leave Windows.
- The Hub has one ingestion bearer token, stored root-only on CentOS and in the Agent platform SecretStore on Windows.
- Android does not receive the ingestion token. It reads the Hub's state/event endpoints over the private network.
- The Hub installer binds to `127.0.0.1:8787` unless an explicit private address is supplied.
- Do not use `0.0.0.0` on an Internet-routable host unless a separate, intentionally designed TLS/authentication/firewall layer exists.

## 2. CentOS prerequisites

Required:

- systemd;
- Python 3.10+ with `venv`/`pip` support;
- a checkout of this repository for installation;
- private network reachability from the Windows machine and Android device;
- permission to create a system user, systemd unit, `/etc/ai-control-hud`, `/usr/local/lib/ai-control-hub`, and `/var/lib/ai-control-hud`.

The installer creates:

```text
/usr/local/lib/ai-control-hub/venv       installed Python environment
/etc/ai-control-hud/hub.env              root:root, mode 0600; Hub configuration/token
/var/lib/ai-control-hud/hub.sqlite3      persistent SQLite state/event log
/etc/systemd/system/ai-control-hub.service
```

The service runs as the unprivileged `ai-control-hub` user. The unit uses `ProtectSystem=strict`, `ProtectHome=true`, an empty capability set, and grants write access only to `/var/lib/ai-control-hud`.

## 3. Choose the private Hub address

For Tailscale/WireGuard, use the CentOS host's overlay IPv4 address. For a trusted LAN, use its fixed LAN address.

Example placeholder used below:

```text
<HUB_PRIVATE_IP>:8787
```

Only the private interface/address should be reachable. Keep host firewall and overlay ACL rules limited to the Windows agent and Android HUD where practical.

Before installation, the rendered unit can be inspected without root or systemd:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit --listen <HUB_PRIVATE_IP>:8787
```

## 4. Generate the ingestion token

Generate a random token on a trusted machine. Do not pass the token itself as a command-line argument.

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Transfer this file to the Windows machine using an existing secure channel. Do not put it in Git, email, issue/PR text, shell arguments, screenshots, or chat messages.

The plaintext token files are temporary import material; delete them after end-to-end validation.

## 5. Install the CentOS Hub

From the repository root on CentOS:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --source "$PWD" \
  --token-file "$HOME/ai-control-hub.token" \
  --listen <HUB_PRIVATE_IP>:8787 \
  --agent-id desktop-main
```

Installation enables the service for boot but intentionally does **not** start it. Inspect the generated configuration first:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
```

Expected properties:

- `/etc/ai-control-hud/hub.env` is root-only (`0600`);
- the service user owns the persistent data directory;
- the unit binds only to the chosen private address;
- the bearer token is not embedded in the unit file.

Start the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

Check reachability from Windows/Android private-network paths:

```bash
curl http://<HUB_PRIVATE_IP>:8787/api/v1/health
```

`/api/v1/health` can be reachable before the Windows agent has uploaded a state snapshot. `/api/v1/state` returns unavailable until the first accepted snapshot arrives.

## 6. Configure the Windows Agent Hub credential

Use the same temporary token file transferred securely to Windows. Run an elevated terminal when the normal machine config is under `%ProgramData%`.

```powershell
.\ai-control-agent.exe hub configure `
  --hub-url http://<HUB_PRIVATE_IP>:8787 `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

On Windows, the sibling Hub secret is protected with machine-scope DPAPI plus the existing file ACL hardening. The machine config schema is unchanged; the default protected Hub record is derived next to `agent.json` as `hub.dpapi`.

The Agent prefers the protected Hub credential when present. Existing `AI_CONTROL_HUB_*` environment variables remain supported only as a development/backward-compatible fallback.

Restart the installed Windows service so it loads the new protected credential:

```powershell
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service status
```

Validate that `/api/v1/state` becomes available through the Hub and that the Hub eventually reports fresh source state.

After validation, securely remove the temporary plaintext token files from both CentOS and Windows.

## 7. Point Android at the Hub

Set the Android HUD server to:

```text
http://<HUB_PRIVATE_IP>:8787
```

The first event request establishes a silent `latestSeq` baseline, so historical completed/failed tasks are not spoken after first installation or a server switch. Subsequent cursors are persisted locally.

Default voice policy:

- completed task: spoken;
- failed task: spoken;
- quiet hours: 23:00–08:00;
- events older than 10 minutes: no individual replay; one catch-up summary after synchronization.

## 8. Hub status and logs

```bash
bash scripts/ai-control-hub-systemd.sh status
sudo journalctl -u ai-control-hub.service --since today
```

Do not add bearer tokens or raw environment dumps to diagnostics or issue reports.

## 9. Online-safe SQLite backup

Do not copy a live WAL-mode SQLite database with a plain `cp` and assume it is consistent. The installed Python environment provides an online backup command based on SQLite's backup API plus `integrity_check`.

Create a protected backup directory once:

```bash
sudo install -d -o ai-control-hub -g ai-control-hub -m 0750 /var/lib/ai-control-hud/backups
```

Create a timestamped backup while the Hub is running:

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/venv/bin/ai-control-hub-backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/hub-${stamp}.sqlite3"
```

Backup files are created mode `0600`. Copy selected backups to a separate host/storage target using your existing protected backup channel. Retention policy is intentionally operator-owned; the application does not silently delete backups.

## 10. Restore a Hub backup

A restore is a maintenance operation and requires stopping the Hub.

1. Stop the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh stop
```

2. Preserve the current database if it is still readable:

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/venv/bin/ai-control-hub-backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/pre-restore-${stamp}.sqlite3"
```

3. Install the selected backup and remove stale WAL sidecars while the service is stopped:

```bash
sudo rm -f /var/lib/ai-control-hud/hub.sqlite3-wal /var/lib/ai-control-hud/hub.sqlite3-shm
sudo install -o ai-control-hub -g ai-control-hub -m 0600 \
  /path/to/validated-backup.sqlite3 \
  /var/lib/ai-control-hud/hub.sqlite3
```

4. Start the Hub and validate `/health`, `/state`, and event flow:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

If the restored event log has a lower high-water mark than Android's persisted cursor, the H5 client detects that reset and silently rebases to the Hub's `latestSeq` instead of remaining stuck.

## 11. Bearer-token rotation

The Hub currently accepts one ingestion token at a time, so rotation creates a brief authentication transition. Keep the Windows Agent running during the server-side change: local collection/event observation continues and failed deliveries remain in the durable outbox.

1. Generate a new token file and transfer it securely to Windows as in section 4.

2. Rotate the CentOS Hub token. If the Hub is active, this action atomically replaces the root-only env file and restarts the service:

```bash
bash scripts/ai-control-hub-systemd.sh rotate-token \
  --token-file "$HOME/ai-control-hub-next.token"
```

3. Import the new token into Windows DPAPI. URL and agent ID are preserved when only `--hub-token-file` is supplied:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-token-file C:\Temp\ai-control-hub-next.token
```

4. Restart the Windows service to load the new record:

```powershell
.\ai-control-agent.exe service restart
```

5. Validate state refresh and a new event delivery, then delete both new plaintext token files.

During the brief token mismatch the Agent may log Hub HTTP 401 errors, but local source collection and durable event observation remain independent from the network delivery loop.

## 12. Outage/reboot acceptance checklist

These are the real-environment H3/H6 checks. Record exact timestamps and results; do not infer success from CI alone.

### Windows shutdown / service stop

1. Verify Hub `/state` is fresh.
2. Stop the Windows Agent service or shut down Windows.
3. After the configured stale threshold (default 45 seconds), verify Hub `/state` retains last-known trustworthy data but source health is projected `stale`/overall `degraded`.
4. Verify Android remains connected to the Hub and shows degraded/stale state rather than server-offline.
5. Restart Windows/Agent and verify state returns fresh.

### Hub/network outage

1. Keep the Windows Agent running.
2. Stop the Hub or temporarily block the private route.
3. Verify the local Windows `/api/v1/state` remains usable.
4. Cause a task terminal transition if safe in the test environment.
5. Restore Hub connectivity and verify the event arrives once in the Hub log despite at-least-once retries/idempotency.

### Android reconnect

1. Disconnect Android from the private network while events occur.
2. Reconnect within 10 minutes and verify new events are consumed from the persisted cursor.
3. For a deliberately older backlog, verify individual old events are not spoken and one catch-up summary is produced outside quiet hours.

### CentOS reboot

1. Create a current database backup.
2. Reboot CentOS.
3. Verify `ai-control-hub.service` starts automatically.
4. Verify the same SQLite state/event log is present and Windows resumes uploads without reconfiguration.

### Backup/restore drill

1. Create an online backup while the Hub is live.
2. Restore it using section 10 on a maintenance window or disposable host.
3. Verify SQLite integrity, state retrieval, event cursor behavior, and Android rebase behavior where applicable.

## 13. Removal

Remove only the systemd registration while retaining app/config/data:

```bash
bash scripts/ai-control-hub-systemd.sh remove
```

Remove service registration, installed venv, and Hub secret configuration while retaining persistent SQLite data:

```bash
bash scripts/ai-control-hub-systemd.sh remove --purge
```

Permanently remove all Hub state as well:

```bash
bash scripts/ai-control-hub-systemd.sh remove --purge --purge-data
```

`--purge-data` is destructive and requires `--purge` explicitly.
