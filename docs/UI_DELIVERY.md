# Agent UI delivery and resource acceptance

Status: UI8 release-gate contract

Scope: native Windows Agent UI + Android portrait/landscape Agent HUD. Central Hub administration UI is not part of this deliverable.

## Windows native UI artifact

CI produces a deterministic delivery archive:

```text
ai-control-agent-ui-windows-amd64.zip
├── ai-control-agent-ui.exe
├── resource-metrics.json
├── README.md
├── BUILD_INFO.txt
└── SHA256SUMS
```

The outer ZIP also receives a SHA-256 sidecar. `BUILD_INFO.txt` records the component, version, source commit, architecture and native runtime stack.

Release builds use MSVC static runtime and `/Brepro`; CI clean-rebuilds the same Release target and requires identical executable SHA-256 values before packaging.

The final native stack remains:

```text
C++20 + Win32 + Direct2D + DirectWrite + WinHTTP
```

No Electron, WebView2, Wails, Qt WebEngine, Flutter or React Native runtime is permitted.

## Windows resource gates

The release executable is started on a Windows hosted runner and measured for:

- Working Set;
- Private Bytes;
- process handles;
- OS threads;
- GDI objects;
- USER objects;
- visible and hidden process CPU time.

CI exercises both resource-sensitive UI7 transitions:

- repeated Mini HUD -> Full Dashboard -> Mini HUD cycles;
- repeated visible -> hidden -> visible cycles.

Initial hard growth limits are leak guards rather than final steady-state budgets:

```text
memory growth     <= 16 MiB per measured phase
handles growth    <= 16
threads growth    <= 4
GDI growth        <= 4
USER growth       <= 4
visible CPU       <= 0.35 process-seconds / 4 wall-seconds
hidden CPU        <= 0.12 process-seconds / 4 wall-seconds
```

The preferred visible working-set target remains 48 MiB and is recorded as a soft warning until several runner measurements establish stable variance.

The UI keeps `/W4 /WX` globally. The current release branch permits exactly one source-local C4456 exception for `src/app.cpp`; CI rejects any additional `/wdXXXX` warning suppression. This exception is limited to the Mini HUD geometry scratch-variable scope and does not relax any other translation unit.

## Native artifact privacy gate

The Windows executable is scanned for known credential/private-local-state markers. CI first proves the scanner by generating a synthetic leaking file that must fail.

The UI must never embed or expose:

- Hub bearer tokens;
- CommandCode API keys;
- DPAPI/SecretStore payloads;
- private ZCode configuration/database paths;
- `%ProgramData%` credential paths.

Service mutations remain delegated to the existing Go Agent CLI through explicit UAC elevation.

## Android delivery

Android CI rebuilds the final UI8 head and verifies both display modes:

```text
res/layout/activity_main.xml       portrait dashboard
res/layout-land/activity_main.xml  landscape focus dashboard
```

The same gate runs:

- `lintDebug` and `lintRelease`;
- debug/release JVM tests;
- debug and unsigned release assembly;
- APK credential scan;
- ephemeral CI release signing;
- `apksigner verify`;
- versionName/versionCode verification;
- portrait + landscape packaged-resource verification.

The CI signing key is disposable and is not a production distribution key.

## Runtime boundaries

- The Windows UI remains a separate per-user process. Its failure/exit does not stop the SCM Agent service.
- Android stays Java/XML/framework Views and API 23 compatible.
- Neither UI reads Hub/Agent SQLite files directly to populate cards.
- Missing source values remain unavailable rather than being fabricated as zero.
- No fixed-FPS render loop or permanent animation is introduced.
- Central Hub remains headless.
