# H3 Real-Device Validation

Status: field acceptance checklist for Central Hub V2

This checklist is intentionally for the actual CentOS host, Windows development machine, and Android device. CI cannot claim these checks on behalf of the real network, service manager, device TTS engine, or power/reboot path.

Use the validation artifacts built from the same commit whenever possible:

- CentOS: `ai-control-hub-h3-validation.zip`;
- Windows: `release-windows-amd64` / `ai-control-agent.exe`;
- Android: `ai-control-hud-debug-apk` / `app-debug.apk`.

Do **not** paste bearer tokens, raw secret files, DPAPI blobs, vendor credentials, or complete environment dumps into test reports. Report only redacted commands/output and timestamps.

## Acceptance summary

H3 is accepted when all mandatory checks below pass:

- Hub starts on the intended private address and survives a CentOS reboot;
- Windows Agent uploads fresh state using the protected Hub credential;
- Android reads Hub state/events and local TTS can speak a test notification;
- stopping/shutting down Windows produces stale/degraded state without making the Hub disappear;
- restoring Windows returns state to fresh without reconfiguration;
- a Hub/network outage does not stop local Windows collection and queued terminal events catch up after recovery;
- Android reconnect resumes from its persisted cursor without replaying historical terminal events;
- an online SQLite backup can be created and passes integrity validation.

Backup restore is recommended for H3 acceptance and mandatory before calling the deployment production-ready.

## 0. Record the test environment

Record these non-secret values:

```text
Validation commit: <commit sha>
CentOS/RHEL release: <version>
CentOS private/overlay IP: <ip>
Python: <python3 --version>
Windows version: <version>
Windows Agent version/build: <version or artifact name>
Android device/model: <model>
Android version: <version>
Private transport: LAN / Tailscale / WireGuard
Test start UTC: <timestamp>
```

Confirm both Windows and Android can route to `<HUB_PRIVATE_IP>:8787` over the intended private network. Do not open the port to the public Internet for this test.

## 1. CentOS Hub install

Unpack `ai-control-hub-h3-validation.zip` on the CentOS host and enter the extracted directory.

Generate a temporary ingestion token on CentOS:

```bash
umask 077
openssl rand -hex 32 > "$HOME/ai-control-hub.token"
chmod 600 "$HOME/ai-control-hub.token"
```

Inspect the unit before installation:

```bash
bash scripts/ai-control-hub-systemd.sh render-unit --listen <HUB_PRIVATE_IP>:8787
```

Expected: `ExecStart` binds to `<HUB_PRIVATE_IP>:8787`; the unit does not contain the bearer token.

Install:

```bash
bash scripts/ai-control-hub-systemd.sh install \
  --source "$PWD" \
  --token-file "$HOME/ai-control-hub.token" \
  --listen <HUB_PRIVATE_IP>:8787 \
  --agent-id desktop-main
```

Inspect permissions:

```bash
sudo systemctl cat ai-control-hub.service
sudo stat -c '%A %U:%G %n' /etc/ai-control-hud/hub.env /var/lib/ai-control-hud
```

Expected:

- `/etc/ai-control-hud/hub.env` is root-only (`0600`);
- `/var/lib/ai-control-hud` is owned by the Hub service user;
- the token is not visible in the unit file.

Start and verify:

```bash
bash scripts/ai-control-hub-systemd.sh start
bash scripts/ai-control-hub-systemd.sh status
curl -fsS http://<HUB_PRIVATE_IP>:8787/api/v1/health
```

PASS criterion: service is active and `/api/v1/health` returns successfully from the private route.

## 2. Windows Agent protected Hub credential

Securely copy the same temporary token file to Windows, for example `C:\Temp\ai-control-hub.token`.

From an elevated PowerShell in the Windows Agent directory:

```powershell
.\ai-control-agent.exe hub configure `
  --hub-url http://<HUB_PRIVATE_IP>:8787 `
  --hub-agent-id desktop-main `
  --hub-token-file C:\Temp\ai-control-hub.token

.\ai-control-agent.exe hub status
```

Expected: status reports the Hub configuration without printing the token.

Restart the installed Agent service:

```powershell
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service status
```

Then from CentOS or another private client:

```bash
curl -fsS http://<HUB_PRIVATE_IP>:8787/api/v1/state
curl -fsS 'http://<HUB_PRIVATE_IP>:8787/api/v1/events?after=0&limit=1'
```

PASS criterion: `/state` becomes available and reflects current Windows-collected data. Hub/Agent logs must not expose the bearer token.

After the end-to-end path is proven, delete both plaintext token import files. Keep the protected CentOS env and Windows DPAPI record.

## 3. Android install and TTS

Install `app-debug.apk` on the Android device. If updating an older debug build with a different signing identity, uninstall the older build first.

Set the Hub server URL in the HUD to:

```text
http://<HUB_PRIVATE_IP>:8787
```

Verify:

1. HUD state loads from the CentOS Hub;
2. voice status reaches ready state;
3. press the test-voice control and confirm audible speech;
4. completed and failed voice toggles can be changed independently;
5. quiet-hours control is visible and defaults to 23:00–08:00 when enabled.

PASS criterion: state is usable and the device TTS engine produces audible local speech.

The first event connection should silently baseline at the Hub's `latestSeq`; historical terminal events must not all speak on first install/server switch.

## 4. Windows stop/shutdown -> stale -> recovery

Start with `/state` fresh and Android showing the same current state.

Stop the Agent service (or shut down Windows for the full power-path test):

```powershell
.\ai-control-agent.exe service stop
```

Record UTC time. After at least the configured stale threshold (default 45 seconds), query:

```bash
curl -fsS http://<HUB_PRIVATE_IP>:8787/api/v1/state
```

Expected:

- last trustworthy data is retained;
- source health becomes `stale`;
- aggregate state becomes `degraded`;
- Android remains connected to the Hub rather than showing the Hub itself as offline.

Restart Windows/Agent:

```powershell
.\ai-control-agent.exe service start
.\ai-control-agent.exe service status
```

PASS criterion: state returns to fresh automatically without reconfiguring the Hub credential.

## 5. Hub/network outage and durable catch-up

Keep Windows and local collection running. Stop the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh stop
```

On Windows, verify the local diagnostic API remains usable. Cause one safe task completion/failure transition in the test environment while the Hub is unavailable.

Restore the Hub:

```bash
bash scripts/ai-control-hub-systemd.sh start
```

Verify the terminal event appears through the Hub event API after connectivity returns. Repeated Agent retries must not create duplicate Hub rows for the same stable `eventId`.

PASS criterion: local collection survives the outage and the queued event eventually arrives once semantically.

## 6. Android cursor/reconnect behavior

Disconnect Android from the private network while one or more events occur, then reconnect.

PASS criterion for a short outage (<10 minutes): new events are consumed from the saved cursor and eligible terminal events speak according to local policy.

For an intentionally old backlog (>10 minutes), PASS criterion: old events are not spoken individually; after catch-up there is at most one summary outside quiet hours.

## 7. CentOS reboot persistence

Before reboot, record the current event high-water mark and create a backup as in section 8.

Reboot CentOS:

```bash
sudo reboot
```

After the host returns:

```bash
systemctl is-enabled ai-control-hub.service
systemctl is-active ai-control-hub.service
curl -fsS http://<HUB_PRIVATE_IP>:8787/api/v1/health
curl -fsS http://<HUB_PRIVATE_IP>:8787/api/v1/state
```

PASS criterion: service autostarts, the same SQLite state/event history remains available, and Windows resumes uploads without reconfiguration.

## 8. Online backup

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

Validate it independently:

```bash
python3 - <<'PY'
import sqlite3, sys, glob
p = sorted(glob.glob('/var/lib/ai-control-hud/backups/hub-*.sqlite3'))[-1]
con = sqlite3.connect(f'file:{p}?mode=ro', uri=True)
print(con.execute('PRAGMA integrity_check').fetchone()[0])
con.close()
PY
```

PASS criterion: backup command succeeds and `integrity_check` prints `ok`.

For the full restore drill, follow `docs/HUB_DEPLOYMENT.md` section 10 during a maintenance window or on a disposable host.

## 9. Result template

Reply with this table or paste it into issue #62. Do not include secrets.

```text
Commit/artifacts:
- H6/H3 commit: <sha>
- Windows artifact: <name>
- Android artifact: <name>

CentOS Hub install: PASS / FAIL
Private reachability: PASS / FAIL
Windows protected credential: PASS / FAIL
Windows -> Hub fresh state: PASS / FAIL
Android state cutover: PASS / FAIL
Android test TTS audible: PASS / FAIL
Windows stop/shutdown stale projection: PASS / FAIL
Windows recovery to fresh: PASS / FAIL
Hub outage local collection survives: PASS / FAIL
Durable event catch-up: PASS / FAIL
Android cursor reconnect: PASS / FAIL
Old-event summary policy: PASS / FAIL / NOT RUN
CentOS reboot/autostart/persistence: PASS / FAIL
Online backup + integrity_check: PASS / FAIL
Restore drill: PASS / FAIL / NOT RUN

Observed timestamps / redacted notes:
- ...
```

For any FAIL, include the failing command, exit code, relevant redacted log lines, and UTC timestamp. Do not retry destructive purge/restore operations until the failure is understood.
