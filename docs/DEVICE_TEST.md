# Target-device smoke test

This procedure validates the native Android client before production ZCode and CommandCode adapters are enabled.

## 1. Start the backend in explicit fixture mode

On the development machine:

```bash
git pull
python -m venv .venv
source .venv/bin/activate
pip install -e '.[dev]'
HUD_FIXTURE=healthy.json python -m server
```

The server listens on `0.0.0.0:8787`.

Fixture mode must be selected explicitly. Normal `python -m server` startup leaves unconfigured vendor sources `disabled`; it does not pretend that real sources are healthy.

## 2. Confirm the backend locally

```bash
curl http://127.0.0.1:8787/api/v1/health
curl http://127.0.0.1:8787/api/v1/state
```

The healthy fixture should report schema version 1 and aggregate `live` state.

## 3. Install the debug APK

Use the `ai-control-hud-debug-apk` artifact produced by the Android CI workflow. Enable installation from the file-manager/browser source on the target Android device when Android requests it.

The debug APK is for development validation only. Persistent release signing is a later release task.

## 4. Connect over the trusted LAN

Put the development machine and phone on the same Wi-Fi/LAN. Find the development machine's LAN IPv4 address and enter this in the app:

```text
http://<development-machine-ip>:8787
```

The app tests `/api/v1/health` before saving the server address.

Do not expose port 8787 directly to the public Internet. Remote access should use a private overlay/TLS design later.

## 5. Healthy-state checks

Verify on the target phone:

- app enters full-screen/immersive display;
- screen stays on while the Activity is foregrounded;
- top state reads `LIVE`;
- ZCode summary and bounded task cards render without flashing;
- at most four task cards are allocated/displayed, with overflow count for additional tasks;
- CommandCode plan/credit/5h/weekly indicators render;
- reset countdown changes locally each second while HTTP polling remains at the normal cadence;
- failed/running/waiting tasks are visually distinguishable.

## 6. Failure-state checks

### Backend offline

Stop the Python server while the app is open. Verify that the client becomes `OFFLINE`, retains a clear last-known display state, backs off retries, and reconnects automatically after the server restarts.

### Degraded source

Restart with a committed degraded fixture, for example:

```bash
HUD_FIXTURE=commandcode_auth_error.json python -m server
```

Verify that the aggregate state becomes `DEGRADED`, ZCode can remain usable, and CommandCode shows its source error rather than fake zero quota.

### Failed task

```bash
HUD_FIXTURE=task_failed.json python -m server
```

Verify that a failed task receives the strongest task warning and is prioritized ahead of less urgent tasks.

## 7. Old-device observations

For at least one extended foreground session, record:

- Android version/device model;
- approximate app memory use if available;
- responsiveness while the dashboard updates;
- device temperature/charging behavior;
- whether the screen remains on reliably;
- behavior after Wi-Fi loss/recovery;
- whether Android battery optimization kills or suspends the app;
- any visible flashing, layout churn, or slow scrolling.

These observations are evidence for issues 5, 8, and 10. Do not close the old-device acceptance work based on CI alone.
