# Windows Shadow Test — Python 8787 vs Go 8788

This procedure validates V2/G4 without changing the Android phone or the production listener.

## Safety model

- Python remains the reference backend on `0.0.0.0:8787`.
- Go binds only to `127.0.0.1:8788` during shadow validation.
- Android continues talking to Python/8787.
- Both processes read ZCode sources read-only.
- The Go process reuses the existing gitignored `.local/commandcode-provider.json` through `HUD_ZCODE_CONFIG`.
- The comparison output contains normalized HUD state only; it never includes the provider API key.

## 1. Update the repository

```powershell
git pull
```

## 2. Keep the Python reference running

If it is not already running:

```powershell
.\scripts\run-windows.ps1
```

Verify:

```powershell
curl http://127.0.0.1:8787/api/v1/state
```

## 3. Place the CI-built Go executable

Create the local binary directory:

```powershell
New-Item -ItemType Directory -Force .local\bin | Out-Null
```

Extract/copy the CI artifact executable to:

```text
.local\bin\ai-control-agent.exe
```

The entire `.local/` tree is ignored by Git.

## 4. Start the Go candidate

Open a second PowerShell in the repository root:

```powershell
.\scripts\run-go-shadow.ps1
```

Expected startup line resembles:

```text
[agent] listen=http://127.0.0.1:8788 schema=1 fixture=false zcode=enabled commandCode=enabled
```

Do not point the Android app to 8788 yet.

Verify the Go endpoint independently:

```powershell
curl http://127.0.0.1:8788/api/v1/state
```

## 5. Compare normalized semantics

Open a third PowerShell:

```powershell
.\scripts\compare-shadow.ps1 -OutputPath .local\shadow-compare.json
```

The comparison intentionally ignores fields that must naturally differ between the two processes, including server uptime and observation timestamps.

Exact checks cover:

- schema version;
- overall/source health;
- ZCode summary counts;
- active task title/workspace/status/activity;
- CommandCode plan/credit limit/unit.

Numeric deltas are reported, not treated as exact equality, for:

- remaining credit;
- 5-hour usage percentage;
- weekly usage percentage.

Those values can move between the two collection instants.

## 6. G4 acceptance observations

Repeat comparison during at least these states before Windows cutover:

1. active Goal running;
2. Goal transition or completion;
3. idle period;
4. Go process restart while Python remains alive;
5. one temporary source failure/recovery test after normal parity is proven.

The migration does not advance to port 8787 merely because the binaries compile. Semantic parity on the target Windows machine is the gate.

## Rollback

There is no production rollback operation during shadow mode because Python remains untouched on port 8787. Stop the Go process with `Ctrl+C` and Android behavior remains unchanged.
