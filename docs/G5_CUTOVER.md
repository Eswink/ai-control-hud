# G5 Windows Cutover

Status: preparation.

G4 semantic parity is complete. G5 moves the production listener from Python to the Go agent without changing the Android schema-v1 contract or configured server URL.

## Preconditions

- G4 is complete.
- `.local/bin/ai-control-agent.exe` is the CI-built Windows artifact from the latest validated Go commit.
- `.local/commandcode-provider.json` remains local and gitignored.
- Python rollback remains available through `scripts/run-windows.ps1` until G6 service integration is stable.

## Cutover invariant

Only one process may listen on TCP 8787.

Before starting Go production mode:

1. stop Python 8787;
2. stop any Go shadow process on 8788 if it is no longer needed;
3. start Go on `0.0.0.0:8787`;
4. verify `/api/v1/state` locally;
5. verify the existing Android device reconnects without changing its configured backend URL.

## Rollback

Stop the Go process with `Ctrl+C`, then run:

```powershell
.\scripts\run-windows.ps1
```

The Android configuration remains unchanged during both cutover and rollback.
