# G5 Windows Cutover

Status: active.

G4 semantic parity is complete. G5 moves the production listener from Python to the Go agent without changing the Android schema-v1 contract or configured server URL.

## Preconditions

- G4 is complete.
- `.local/bin/ai-control-agent.exe` is the CI-built Windows artifact from the latest validated Go commit.
- `.local/commandcode-provider.json` remains local and gitignored.
- Python rollback remains available through `scripts/run-windows.ps1` until G6 service integration is stable.

## Safety invariant

Only one process may listen on TCP 8787. `scripts/run-go-windows.ps1` refuses to start if 8787 is already occupied.

The Go production process binds `0.0.0.0:8787`; the Android device keeps its existing backend URL and API contract.

## Cutover

1. Update the repository:

```powershell
git pull
```

2. Stop the Python 8787 terminal with `Ctrl+C`.

3. Stop the old Go shadow 8788 terminal if it is no longer needed.

4. Start Go production mode:

```powershell
.\scripts\run-go-windows.ps1
```

Expected startup includes:

```text
[go] Production listener: http://0.0.0.0:8787
[agent] listen=http://0.0.0.0:8787 ...
```

If Windows Firewall prompts, allow the Go executable on **Private networks** so the Android device can reach it over LAN.

5. In another PowerShell, verify the local endpoint:

```powershell
curl http://127.0.0.1:8787/api/v1/state
.\scripts\verify-go-cutover.ps1
```

The verifier writes `.local/g5-cutover.json` and requires:

- schemaVersion 1;
- Go server identity;
- overall `live`;
- ZCode `ok`;
- CommandCode `ok`.

6. Verify the existing Android device reconnects without changing its configured server URL. Confirm current Goal/activity and CommandCode usage still update.

7. Keep Go running for a meaningful observation window. G5 is complete only after the Android device operates normally with no Python process required.

## Rollback

Stop the Go process with `Ctrl+C`, then run:

```powershell
.\scripts\run-windows.ps1
```

Verify:

```powershell
curl http://127.0.0.1:8787/api/v1/state
```

The Android configuration remains unchanged during both cutover and rollback.
