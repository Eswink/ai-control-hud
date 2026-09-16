# Windows Shadow Test — Python 8787 vs Go 8788

This procedure validates V2/G4 without changing the Android phone or the production listener.

## Safety model

- Python remains the reference backend on `0.0.0.0:8787`.
- Go binds only to `127.0.0.1:8788` during live shadow validation.
- Android continues talking to Python/8787.
- Both processes read ZCode sources read-only.
- The Go process reuses the existing gitignored `.local/commandcode-provider.json` through `HUD_ZCODE_CONFIG`.
- The comparison output contains normalized HUD state only; it never includes the provider API key.

## Live-source parity

### 1. Update the repository

```powershell
git pull
```

### 2. Keep the Python reference running

If it is not already running:

```powershell
.\scripts\run-windows.ps1
```

Verify:

```powershell
curl http://127.0.0.1:8787/api/v1/state
```

### 3. Place the CI-built Go executable

Create the local binary directory:

```powershell
New-Item -ItemType Directory -Force .local\bin | Out-Null
```

Extract/copy the CI artifact executable to:

```text
.local\bin\ai-control-agent.exe
```

The entire `.local/` tree is ignored by Git.

### 4. Start the Go candidate

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

### 5. Compare normalized semantics

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

## Deterministic lifecycle parity

Long-running real Goal loops are **not** a release gate. A Goal that naturally runs for many hours must not force the migration to wait for a terminal transition.

After live normal-work parity and a Go backend restart have passed, run the isolated lifecycle harness instead:

```powershell
.\scripts\g4-deterministic-parity.ps1
```

The harness uses only local test resources:

- Python on `127.0.0.1:8797`;
- Go on `127.0.0.1:8798`;
- a temporary synthetic Goal SQLite database under `.local\g4-parity`;
- an empty synthetic task index;
- a provider config with no providers, so no real CommandCode credential or billing request is used.

It automatically proves these states against both implementations:

```text
running
  -> source files unavailable
  -> stale + last-known-good
  -> source files restored
  -> ok + recovered running state
  -> completed
  -> idle with zero current tasks
```

It writes per-stage comparisons plus:

```text
.local\g4-parity\summary.json
```

The harness never binds 8787/8788, never writes the real ZCode database, never changes the Android server URL, and never touches the real provider mirror.

## G4 acceptance

Before Windows cutover, require all of the following:

1. real target-machine normal-work parity on Python 8787 vs Go 8788;
2. Go backend restart while Python remains alive, followed by parity again;
3. deterministic `running -> completed -> idle` parity using the isolated harness;
4. deterministic source failure -> `stale + last-known-good` -> recovery parity using the isolated harness;
5. no unexplained semantic drift.

A natural completion of a many-hour real Goal is useful additional evidence, but it is not required to unblock the migration.

## Rollback

There is no production rollback operation during shadow mode because Python remains untouched on port 8787. Stop the Go process with `Ctrl+C` and Android behavior remains unchanged.
