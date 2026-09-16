# H3 Real-Device Validation

Status: field acceptance checklist for Central Hub V2 + LAN auto-discovery

This checklist is for the actual CentOS host, Windows development machine, and Android device. CI validates protocol/build behavior; only the real LAN can validate broadcast delivery, DHCP address changes, device TTS, reboot, and power-loss paths.

The preferred intranet deployment uses:

- Hub HTTP: TCP `8787`;
- Hub discovery: UDP `8788`;
- Windows protected credential mode: `auto://lan`;
- Android configured identity: `http://auto.lan`;
- current observed Hub address during development: `192.168.101.103` (informational only; never hard-code it).

LAN discovery uses UDP broadcast and normally requires clients and the Hub to share a broadcast domain. If the network/AP blocks broadcast or uses client isolation, use the manual-address fallback documented below.

Do **not** paste bearer tokens, raw secret files, DPAPI blobs, vendor credentials, or complete environment dumps into test reports.

## Acceptance summary

H3 is accepted when the mandatory checks below pass:

- Hub starts in `--lan-auto` mode and advertises itself without exposing secrets;
- Windows Agent automatically resolves the Hub and uploads fresh state;
- Android automatically resolves the Hub and displays the actual current Hub URL;
- Windows/Android recover automatically if the Hub DHCP address changes;
- stopping/shutting down Windows produces stale/degraded state while Hub remains available;
- Hub/network outage preserves local collection and durable terminal events catch up after recovery;
- Android reconnect resumes from its stable event cursor;
- CentOS reboot preserves service/state;
- online SQLite backup passes integrity validation.

## 0. Record the test environment

Record non-secret values:

```text
Validation commit: <commit sha>
CentOS/RHEL release: <version>
Current Hub IPv4: <current address; initially expected around 192.168.101.103>
Python: <python3 --version>
Windows version: <version>
Windows Agent artifact/version: <value>
Android model/version: <value>
Network: trusted LAN
Test start UTC: <timestamp>
```

Ensure TCP `8787` and UDP `8788` are allowed between these LAN devices. Do not expose either port to the public Internet.

## 1. Install CentOS Hub with automatic LAN discovery

Unpack `ai-control-hub-h3-validation.zip`, enter its root, and generate a temporary ingestion token:

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Inspect the unit before installation:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit --lan-auto --hub-id dorm-hub
```

Expected:

- HTTP binds `0.0.0.0:8787` because LAN mode was explicitly selected;
- discovery is enabled on UDP `8788`;
- the bearer token is not embedded in the unit.

Install and start:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --source "$PWD" \
  --token-file "$HOME/ai-control-hub.token" \
  --lan-auto \
  --hub-id dorm-hub \
  --agent-id desktop-main

bash scripts/ai-control-hub-systemd.sh start
bash scripts/ai-control-hub-systemd.sh status
```

The installer prints candidate LAN URLs when possible. On the current network one may be:

```text
http://192.168.101.103:8787
```

Treat that address as observed state, not configuration.

If `firewalld` is active, allow TCP `8787` and UDP `8788` only on the trusted/private LAN zone/interface according to the host's firewall policy.

Verify permissions:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
```

Expected: `hub.env` is `0600` root-only; the data directory belongs to the Hub service user.

Use the currently observed IP for a direct HTTP sanity check:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/health
```

## 2. Configure Windows Agent for auto-discovery

Securely copy the same temporary token file to Windows. From an elevated PowerShell in the Agent directory:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-auto `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

Expected status includes:

```text
url=auto://lan
resolved=http://<CURRENT_HUB_IP>:8787
hub=dorm-hub
```

The token must not be printed.

Restart the installed service:

```powershell
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service status
```

Then verify through the current Hub address:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/state
curl -fsS 'http://<CURRENT_HUB_IP>:8787/api/v1/events?after=0&limit=1'
```

PASS: fresh Windows-collected state appears through the Hub without a hard-coded Hub IP in the protected Windows record.

After the end-to-end path is proven, delete the temporary plaintext token files from CentOS and Windows. Keep `/etc/ai-control-hud/hub.env` and the Windows protected Hub record.

## 3. Android automatic discovery and address display

Install the latest H3 Android APK.

For a **fresh install**, auto-discovery is the default. For an existing installation with a saved manual URL, open `SERVER`, enter:

```text
auto.lan
```

and connect.

Expected dashboard server label after discovery:

```text
AUTO · http://<CURRENT_HUB_IP>:8787
```

On the current LAN this may initially display:

```text
AUTO · http://192.168.101.103:8787
```

Verify:

1. state loads from the CentOS Hub;
2. the actual discovered address is visible in the dashboard;
3. TTS reaches ready state;
4. `TEST VOICE` is audible;
5. completed/failed speech switches work independently;
6. quiet hours remain configurable.

The stable configured identity remains `http://auto.lan`, so a DHCP IP change does **not** reset the Android event cursor.

## 4. DHCP/IP-change rediscovery

This is the new mandatory discovery check when it can be performed safely.

Change the CentOS LAN address through DHCP renewal/reservation change or a controlled maintenance re-address. Do not modify the Windows Hub credential or Android server setting.

Record:

```text
Old Hub IP: <old>
New Hub IP: <new>
```

Expected:

- old HTTP requests fail after the address move;
- Windows Agent re-runs discovery and resumes upload to the new address;
- `ai-control-agent.exe hub status` displays the new `resolved=` URL;
- Android re-runs discovery after request failure and resumes polling;
- Android server label updates to `AUTO · http://<new-ip>:8787`;
- Android event cursor continues rather than silently rebasing because only the physical address changed.

If the LAN does not permit safely forcing a DHCP change during this session, mark this `NOT RUN` and perform it before production sign-off.

## 5. Windows stop/shutdown -> stale -> recovery

Start with Hub `/state` fresh. Stop the Agent service or shut down Windows:

```powershell
.\ai-control-agent.exe service stop
```

After at least the default stale threshold (45 seconds), query the current discovered Hub URL:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/state
```

Expected:

- last trustworthy data remains;
- source health becomes `stale`;
- aggregate state becomes `degraded`;
- Android stays connected to the Hub.

Restart Windows/Agent and verify fresh state returns without Hub reconfiguration.

## 6. Hub/network outage and durable event catch-up

Keep Windows collection running and stop the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh stop
```

Confirm the local Windows `/api/v1/state` still works. Cause one safe completed/failed task transition while Hub is down, then restart Hub:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

PASS: queued event reaches Hub after recovery and stable `eventId` prevents duplicate semantic entries.

## 7. Android reconnect/cursor behavior

Disconnect Android from LAN while events occur, then reconnect.

- short outage (<10 minutes): eligible new events resume from saved cursor and may speak;
- old backlog (>10 minutes): old events are not spoken one-by-one; at most one catch-up summary is produced outside quiet hours.

PASS: reconnect does not replay historical terminal events or lose the cursor.

## 8. CentOS reboot persistence

Create a backup, reboot CentOS, then check:

```bash
systemctl is-enabled ai-control-hub.service
systemctl is-active ai-control-hub.service
```

After networking returns, Windows/Android should discover the Hub again even if DHCP assigned a different address.

PASS: service autostarts, SQLite state/event history remains, and clients resume without manual IP editing.

## 9. Online backup

Create the backup directory once:

```bash
sudo install -d -o ai-control-hub -g ai-control-hub -m 0750 /var/lib/ai-control-hud/backups
```

Create a live backup:

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/venv/bin/ai-control-hub-backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/hub-${stamp}.sqlite3"
```

Validate:

```bash
python3 - <<'PY'
import sqlite3, glob
p = sorted(glob.glob('/var/lib/ai-control-hud/backups/hub-*.sqlite3'))[-1]
con = sqlite3.connect(f'file:{p}?mode=ro', uri=True)
print(con.execute('PRAGMA integrity_check').fetchone()[0])
con.close()
PY
```

PASS: prints `ok`.

## 10. Manual-address fallback

Use this only when UDP broadcast is intentionally unavailable (separate VLAN/subnet, AP client isolation, firewall policy, etc.).

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
  --hub-token-file C:\Temp\ai-control-hub.token
```

Android: enter `http://<FIXED_PRIVATE_IP>:8787` in `SERVER`.

## 11. Result template

```text
Commit/artifacts:
- discovery/H3 commit: <sha>
- Windows artifact: <name>
- Android artifact: <name>

CentOS --lan-auto install: PASS / FAIL
TCP 8787 reachability: PASS / FAIL
UDP 8788 Windows discovery: PASS / FAIL
Windows resolved URL displayed: PASS / FAIL
Windows -> Hub fresh state: PASS / FAIL
Android auto discovery: PASS / FAIL
Android actual Hub URL displayed: PASS / FAIL
Android test TTS audible: PASS / FAIL
DHCP/IP-change Windows rediscovery: PASS / FAIL / NOT RUN
DHCP/IP-change Android rediscovery: PASS / FAIL / NOT RUN
Event cursor preserved across IP change: PASS / FAIL / NOT RUN
Windows stop/shutdown stale projection: PASS / FAIL
Windows recovery to fresh: PASS / FAIL
Hub outage local collection survives: PASS / FAIL
Durable event catch-up: PASS / FAIL
Android cursor reconnect: PASS / FAIL
Old-event summary policy: PASS / FAIL / NOT RUN
CentOS reboot/autostart/persistence: PASS / FAIL
Online backup + integrity_check: PASS / FAIL
Restore drill: PASS / FAIL / NOT RUN

Old Hub IP: <value>
New Hub IP: <value or NOT RUN>
Redacted notes/timestamps:
- ...
```

For a FAIL, include the command, exit code, UTC timestamp, and relevant redacted log lines. Never include tokens or credential material.
