# H3 Real-Device Validation

Status: field acceptance checklist for Central Hub V2 + Go Hub + LAN auto-discovery

The Central Hub runtime is now a standalone Go binary. CentOS does **not** need Python, pip, venv, or a Go toolchain. The Python Hub remains in the repository temporarily as a parity/reference implementation until the Go cutover passes CI and real-device validation.

Preferred intranet deployment:

- Hub HTTP: TCP `8787`;
- Hub discovery: UDP `8788`;
- Windows protected credential mode: `auto://lan`;
- Android configured identity: `http://auto.lan`;
- current observed Hub address during development: `192.168.101.103` (informational only; never hard-code it).

LAN discovery normally requires the Hub, Windows, and Android device to share a broadcast domain. If the LAN/AP blocks broadcast or enables client isolation, use the manual-address fallback.

Do **not** paste bearer tokens, raw secret files, DPAPI blobs, vendor credentials, or complete environment dumps into test reports.

## Acceptance summary

H3 is accepted when the mandatory checks below pass:

- standalone Go Hub installs and starts on the CentOS host without Python runtime dependencies;
- Hub starts in `--lan-auto` mode and advertises itself without exposing secrets;
- Windows Agent automatically resolves the Hub and uploads fresh state;
- Android automatically resolves the Hub and displays the actual current Hub URL;
- Windows/Android recover automatically if the Hub DHCP address changes;
- stopping/shutting down Windows produces stale/degraded state while the Hub remains available;
- Hub/network outage preserves local collection and durable terminal events catch up after recovery;
- Android reconnect resumes from its stable event cursor;
- CentOS reboot preserves service/state;
- Go Hub online SQLite backup succeeds and passes its built-in `integrity_check`.

## 0. Record the environment

```text
Validation commit: <commit sha>
CentOS/RHEL release: <version>
Current Hub IPv4: <current address; initially expected around 192.168.101.103>
Hub binary version: <./ai-control-hub version>
Windows version: <version>
Windows Agent artifact/version: <value>
Android model/version: <value>
Network: trusted LAN
Test start UTC: <timestamp>
```

There is intentionally no Python-version prerequisite for the Go Hub.

Ensure TCP `8787` and UDP `8788` are allowed only on the trusted/private LAN. Do not expose these ports to the public Internet.

## 1. Install the Go Hub with LAN auto-discovery

Unpack `ai-control-hub-h3-validation.zip` and enter the extracted directory. It contains:

```text
ai-control-hub
scripts/ai-control-hub-systemd.sh
docs/H3_FIELD_VALIDATION.md
docs/HUB_DEPLOYMENT.md
BUILD_INFO.txt
SHA256SUMS
```

Verify the bundle and binary:

```bash
sha256sum -c SHA256SUMS
chmod +x ./ai-control-hub
./ai-control-hub version
```

Generate a temporary ingestion token:

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Inspect the unit:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit --lan-auto --hub-id dorm-hub
```

Expected:

- `ExecStart` points to `/usr/local/lib/ai-control-hub/ai-control-hub`;
- no `python`, `uvicorn`, or `venv` appears in `ExecStart`;
- HTTP binds `0.0.0.0:8787` because LAN mode was explicitly selected;
- discovery is enabled on UDP `8788`;
- bearer token is not embedded in the unit.

Install and start:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --binary "$PWD/ai-control-hub" \
  --token-file "$HOME/ai-control-hub.token" \
  --lan-auto \
  --hub-id dorm-hub \
  --agent-id desktop-main

bash scripts/ai-control-hub-systemd.sh start
bash scripts/ai-control-hub-systemd.sh status
```

The installer verifies that the supplied ELF can execute on the host, installs it root-owned, creates the unprivileged service account and persistent data directory, and enables the service for reboot. If an older Python Hub venv exists under `/usr/local/lib/ai-control-hub/venv`, the Go install removes that legacy venv while preserving `/var/lib/ai-control-hud/hub.sqlite3`.

The Go Hub deliberately uses the same SQLite schema as the Python Hub, so an existing Hub database can be reused in place.

The installer prints candidate LAN URLs when possible. On the current network one may be:

```text
http://192.168.101.103:8787
```

Treat that address as observed state, not configuration.

Verify:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/health
```

PASS: service is active, `hub.env` is `0600` root-only, data is owned by the service user, and `/health` returns schema v1.

## 2. Configure Windows Agent for auto-discovery

Copy the same temporary token file to Windows. In elevated PowerShell:

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

The token must never be printed.

Restart the Windows service:

```powershell
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service status
```

Verify through the current Hub address:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/state
curl -fsS 'http://<CURRENT_HUB_IP>:8787/api/v1/events?after=0&limit=1'
```

PASS: fresh state arrives without a hard-coded Hub IP in the Windows protected record.

After the end-to-end path is proven, delete the temporary plaintext token files from both machines. Keep `/etc/ai-control-hud/hub.env` and the Windows DPAPI record.

## 3. Android automatic discovery and displayed address

Install the latest Android H3 APK.

- fresh install: automatic discovery is the default;
- existing install with a saved manual URL: open `SERVER`, enter `auto.lan`, and connect.

Expected dashboard label:

```text
AUTO · http://<CURRENT_HUB_IP>:8787
```

On the current LAN this may initially be:

```text
AUTO · http://192.168.101.103:8787
```

Verify state loading, displayed resolved address, TTS readiness, audible `TEST VOICE`, independent completed/failed speech switches, and quiet-hours control.

The stable Android identity remains `http://auto.lan`; a DHCP IP change therefore does not reset the event cursor.

## 4. DHCP/IP-change rediscovery

Safely change the CentOS LAN address through DHCP renewal/reservation change or controlled re-addressing. Do not change the Windows Hub credential or Android server setting.

Record:

```text
Old Hub IP: <old>
New Hub IP: <new>
```

PASS criteria:

- old HTTP address stops working;
- Windows re-runs discovery and resumes uploads to the new IP;
- `ai-control-agent.exe hub status` shows the new `resolved=` value;
- Android re-discovers and resumes polling;
- Android label updates to `AUTO · http://<new-ip>:8787`;
- Android event cursor continues without silent re-baseline.

If changing DHCP safely is not possible in the current session, mark this `NOT RUN`; it remains required before production sign-off.

## 5. Windows stop/shutdown -> stale -> recovery

Start with `/state` fresh. Stop the Windows Agent or shut down Windows:

```powershell
.\ai-control-agent.exe service stop
```

After at least 45 seconds:

```bash
curl -fsS http://<CURRENT_HUB_IP>:8787/api/v1/state
```

Expected: last trustworthy data remains, healthy sources become `stale`, aggregate state is `degraded`, and Android remains connected to the Hub.

Restart Windows/Agent and verify state returns fresh without reconfiguration.

## 6. Hub/network outage and durable event catch-up

Keep Windows collection running and stop the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh stop
```

Confirm the local Windows `/api/v1/state` still works. Cause one safe completed/failed transition while Hub is unavailable, then start it again:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

PASS: queued event arrives after recovery and stable `eventId` prevents duplicate Hub entries.

## 7. Android reconnect/cursor behavior

Disconnect Android while events occur, then reconnect.

- outage <10 minutes: eligible new events resume from saved cursor;
- old backlog >10 minutes: old events are not spoken individually; at most one catch-up summary is spoken outside quiet hours.

PASS: no historical replay and no cursor loss.

## 8. CentOS reboot persistence

Create a backup, reboot CentOS, then check:

```bash
systemctl is-enabled ai-control-hub.service
systemctl is-active ai-control-hub.service
/usr/local/lib/ai-control-hub/ai-control-hub version
```

After networking returns, Windows/Android should rediscover the Hub even if DHCP assigned a different IP.

PASS: service autostarts, SQLite state/event history remains, and clients resume without manual IP editing.

## 9. Online backup with the Go binary

Create the protected backup directory once:

```bash
sudo install -d -o ai-control-hub -g ai-control-hub -m 0750 /var/lib/ai-control-hud/backups
```

Create a live backup while the Hub is running:

```bash
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
sudo -u ai-control-hub \
  /usr/local/lib/ai-control-hub/ai-control-hub backup \
  --database /var/lib/ai-control-hud/hub.sqlite3 \
  --output "/var/lib/ai-control-hud/backups/hub-${stamp}.sqlite3"
```

Expected output includes:

```text
integrity=ok
```

The Go backup command uses SQLite `VACUUM INTO`, validates `PRAGMA integrity_check`, fsyncs the result, applies mode `0600`, and refuses to overwrite an existing destination.

PASS: backup command succeeds and reports `integrity=ok`.

## 10. Restore drill

Stop the service before replacing the live database:

```bash
bash scripts/ai-control-hub-systemd.sh stop
sudo rm -f /var/lib/ai-control-hud/hub.sqlite3-wal /var/lib/ai-control-hud/hub.sqlite3-shm
sudo install -o ai-control-hub -g ai-control-hub -m 0600 \
  /path/to/validated-backup.sqlite3 \
  /var/lib/ai-control-hud/hub.sqlite3
bash scripts/ai-control-hub-systemd.sh start
```

Verify `/health`, `/state`, events, and Android cursor behavior. If the restored event high-water mark is lower, Android should silently rebase as designed.

## 11. Manual-address fallback

Use this only when UDP broadcast is unavailable.

CentOS:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --binary "$PWD/ai-control-hub" \
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

## 12. Result template

```text
Commit/artifacts:
- Go Hub/H3 commit: <sha>
- Go Hub version: <value>
- Windows artifact: <name>
- Android artifact: <name>

Go Hub binary runs without Python: PASS / FAIL
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
Go online backup integrity=ok: PASS / FAIL
Restore drill: PASS / FAIL / NOT RUN

Old Hub IP: <value>
New Hub IP: <value or NOT RUN>
Redacted notes/timestamps:
- ...
```

For a FAIL, include the command, exit code, UTC timestamp, and relevant redacted logs. Never include tokens or other credentials.
