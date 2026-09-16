# Central Hub Deployment Runbook

Status: deployment guide for Central Hub V2 (H3/H6/H7)

This guide deploys the AI Control Hub to a 24/7 CentOS/RHEL-family host, keeps ZCode/CommandCode collection on the Windows development machine, and points the Android HUD at the Hub over a trusted private network.

For the current dormitory LAN, **automatic LAN discovery is the preferred mode**. The currently observed server address is `192.168.101.103`, but that address is treated only as runtime information because DHCP may change it.

## 1. Security and discovery model

- Windows is the only place that reads ZCode and CommandCode sources.
- Windows initiates outbound Hub requests; Hub never polls Windows.
- Vendor credentials never leave Windows.
- Hub ingestion uses one bearer token stored root-only on CentOS and in the Agent platform SecretStore on Windows.
- Android does not receive the ingestion token.
- Hub HTTP remains TCP `8787`.
- LAN discovery uses UDP `8788` and advertises only `service`, schema version, Hub ID, HTTP scheme/port, and Hub version. It never advertises a bearer token.
- `--lan-auto` must be selected explicitly; the safe default remains loopback-only.
- UDP broadcast normally works only inside the same broadcast domain. Routed VLANs, AP client isolation, or firewall policy may require manual-address fallback.
- Public Internet exposure is outside this design.

## 2. CentOS prerequisites

Required:

- systemd;
- Python 3.10+ with `venv`/`pip`;
- the H3 validation bundle or repository checkout;
- private-LAN reachability from Windows and Android;
- permission to create the service user/unit and app/config/data directories.

Installed paths:

```text
/usr/local/lib/ai-control-hub/venv       Python environment
/etc/ai-control-hud/hub.env              root:root 0600; Hub token/config
/var/lib/ai-control-hud/hub.sqlite3      persistent state/event log
/etc/systemd/system/ai-control-hub.service
```

The service runs as `ai-control-hub` with systemd hardening and write access only to `/var/lib/ai-control-hud`.

## 3. Generate the ingestion token

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Transfer the same temporary token file to Windows using an existing secure channel. Do not put it in Git, issues, screenshots, chat, or command-line literals. Delete plaintext import files after end-to-end validation.

## 4. Preferred LAN-auto installation

Inspect the unit:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit --lan-auto --hub-id dorm-hub
```

Install:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --source "$PWD" \
  --token-file "$HOME/ai-control-hub.token" \
  --lan-auto \
  --hub-id dorm-hub \
  --agent-id desktop-main
```

`--lan-auto` explicitly does two things:

1. binds Hub HTTP to `0.0.0.0:8787` on this trusted LAN host;
2. starts the UDP discovery responder on port `8788`.

The installer prints candidate LAN URLs when interfaces are already configured. Example current output may include:

```text
http://192.168.101.103:8787
```

That address is informational and can change.

Installation enables service autostart but intentionally leaves it stopped for inspection:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
```

Expected:

- `hub.env` is root-only (`0600`);
- data directory belongs to `ai-control-hub`;
- bearer token is absent from the unit;
- `HUD_HUB_DISCOVERY_ENABLED=1`;
- discovery port is `8788`.

If `firewalld` or another host firewall is active, permit **TCP 8787** and **UDP 8788** only on the trusted/private interface or zone.

Start:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

Direct sanity check using the currently observed address:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/health
```

Before the first Windows upload, `/health` can respond while `/state` is still unavailable.

## 5. Windows Agent auto-discovery

From an elevated PowerShell in the Windows Agent directory:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-auto `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

The protected record stores `auto://lan`, the agent ID, and the bearer token; it does **not** store a DHCP IP.

A successful status command reports both the stable configuration and current resolution, for example:

```text
url=auto://lan resolved=http://192.168.101.103:8787 hub=dorm-hub
```

Restart the service:

```powershell
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service status
```

In auto mode, the uploader caches the discovered address. If an HTTP request fails, it invalidates that physical address, broadcasts discovery again, and retries once against a newly discovered address. Local collection and durable event observation remain independent from discovery/network failures.

## 6. Android auto-discovery

A fresh H7/H3 Android install defaults to the stable configured identity:

```text
http://auto.lan
```

This is an internal sentinel, not DNS. `StateClient` resolves it with the same UDP discovery protocol before HTTP requests.

For an existing installation with an old manual server URL, open `SERVER`, enter:

```text
auto.lan
```

and connect.

The dashboard displays the actual resolved address:

```text
AUTO · http://192.168.101.103:8787
```

When DHCP changes the server address, the app re-discovers after a request failure and updates this label. The event cursor remains keyed to the stable `auto.lan` identity, so changing the physical IP does not reset event history.

## 7. Discovery limitations and manual fallback

Automatic discovery is intentionally simple and local. It may not cross:

- routed subnets/VLANs;
- Wi-Fi client isolation;
- networks that drop UDP broadcast;
- host firewalls that block UDP `8788`.

When that is intentional, use a fixed private address.

CentOS:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --source "$PWD" \
  --token-file "$HOME/ai-control-hub.token" \
  --listen <FIXED_PRIVATE_IP>:8787 \
  --agent-id desktop-main
```

Windows:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-url http://<FIXED_PRIVATE_IP>:8787 `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token
```

Android: configure `http://<FIXED_PRIVATE_IP>:8787`.

## 8. Address-change acceptance test

Before production sign-off, force one safe DHCP/address change if possible.

Record old/new addresses. Do not edit Windows/Android Hub settings.

PASS requires:

- Windows upload resumes automatically at the new address;
- `hub status` reports the new `resolved=` value;
- Android polling resumes automatically;
- Android displays the new actual Hub URL;
- event cursor remains intact.

If a forced address change is unsafe during initial validation, mark it `NOT RUN` and complete it before production sign-off.

## 9. Hub status and logs

```bash
bash scripts/ai-control-hub-systemd.sh status
sudo journalctl -u ai-control-hub.service --since today
```

Do not include bearer tokens or raw environment dumps in diagnostics.

## 10. Online-safe SQLite backup

Do not raw-copy a live WAL-mode database and assume consistency. Use SQLite's backup API:

```bash
sudo install -d -o ai-control-hub -g ai-control-hub -m 0750 /var/lib/ai-control-hud/backups
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/venv/bin/ai-control-hub-backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/hub-${stamp}.sqlite3"
```

The backup command runs an integrity check before publishing the backup and creates mode `0600` files.

## 11. Restore

Stop Hub first:

```bash
bash scripts/ai-control-hub-systemd.sh stop
```

Preserve the current database if readable, then remove stale sidecars and install the validated backup:

```bash
sudo rm -f /var/lib/ai-control-hud/hub.sqlite3-wal /var/lib/ai-control-hud/hub.sqlite3-shm
sudo install -o ai-control-hub -g ai-control-hub -m 0600 \
  /path/to/validated-backup.sqlite3 \
  /var/lib/ai-control-hud/hub.sqlite3
bash scripts/ai-control-hub-systemd.sh start
```

If the restored event log has a lower high-water mark, Android silently rebases its event cursor as designed.

## 12. Bearer-token rotation

Generate and securely transfer a new token file. Rotate server token:

```bash
bash scripts/ai-control-hub-systemd.sh rotate-token \
  --token-file "$HOME/ai-control-hub-next.token"
```

Update only the Windows protected token; auto-discovery mode is preserved:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-token-file C:\Temp\ai-control-hub-next.token
.\ai-control-agent.exe service restart
```

Validate new state/event delivery, then delete both new plaintext token files.

## 13. Outage/reboot acceptance

### Windows shutdown

Stop/shut down Windows. After the default 45-second stale threshold, Hub must retain last-known trustworthy values but project source state as `stale` and aggregate as `degraded`. Android must remain connected to Hub. Restarting Windows must restore fresh state without reconfiguration.

### Hub/network outage

Stop Hub while Windows remains running, create one safe terminal task transition, then restart Hub. Local Windows `/api/v1/state` must remain usable and the durable event must catch up without a duplicate semantic event.

### Android reconnect

Disconnect/reconnect Android. New events resume from the persisted cursor; old (>10 minute) backlog is summarized rather than spoken event-by-event.

### CentOS reboot

After reboot, systemd autostarts Hub. If DHCP returns a different address, Windows/Android must re-discover it automatically.

## 14. Removal

Remove only the systemd registration while preserving app/config/data:

```bash
bash scripts/ai-control-hub-systemd.sh remove
```

Remove app/config but retain SQLite state:

```bash
bash scripts/ai-control-hub-systemd.sh remove --purge
```

Permanently remove all Hub state:

```bash
bash scripts/ai-control-hub-systemd.sh remove --purge --purge-data
```

`--purge-data` is destructive and requires `--purge` explicitly.
