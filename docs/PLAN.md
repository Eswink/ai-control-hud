# Development Plan

## Product objective

Turn an unused Android phone into a low-overhead, always-on engineering HUD showing:

1. Current ZCode task state.
2. CommandCode usage/quota state.
3. Connectivity/freshness of both data sources.
4. Clear failure/warning states that can be read at a glance.

The system is deliberately split into a **Python data plane** and a **native Android display plane**. Vendor-specific logic stays on the development machine; the Android device consumes one stable internal API.

---

## Non-goals for v1

- Reimplementing the full ZCode UI.
- Editing prompts/tasks from the Android device.
- Storing vendor credentials on Android.
- Public Internet exposure by default.
- Complex charts, animation, or historical analytics.
- Depending on undocumented vendor fields directly from UI code.

---

# Milestone 0 — Source discovery and contracts

This milestone is blocking for all vendor-specific implementation.

## M0.1 ZCode source discovery

Inspect the actual local installation and document:

- Candidate SQLite database paths.
- Database schema and schema version indicators.
- Task/session/status tables.
- Running/waiting/failed/completed status representation.
- Workspace/project/title identifiers.
- Timestamps and duration derivation.
- Optional additions/deletions/activity/log data.
- WAL behavior and safe read-only access.
- Behavior when ZCode is running, stopped, upgraded, or database files rotate.

### Deliverable

`docs/sources/ZCODE.md` containing verified paths, schemas, sample sanitized rows, and a compatibility strategy.

## M0.2 CommandCode source discovery

Inspect the actual local installation and determine the safest supported way to retrieve:

- Plan/tier.
- Remaining credit/quota.
- Rolling window usage.
- Reset timestamps.
- Authentication mechanism and token refresh behavior.
- CLI output or local state that can be consumed without exposing credentials.

Undocumented network endpoints must not become the primary contract until verified and isolated behind an adapter.

### Deliverable

`docs/sources/COMMANDCODE.md` with verified source, sanitized sample payloads, refresh rules, failure modes, and credential-handling constraints.

## M0.3 Canonical state model

Freeze an internal DTO independent of both vendors.

Minimum entities:

- `HudState`
- `SourceHealth`
- `TaskSummary`
- `UsageSummary`
- `UsageWindow`

### Exit criteria

- Android code can be implemented against `docs/API.md` without knowing ZCode or CommandCode internals.
- Vendor adapters can change without breaking the Android API.

---

# Milestone 1 — Python backend MVP

## M1.1 Project skeleton

Create:

```text
server/
  app.py
  config.py
  state.py
  models.py
  adapters/
    base.py
    zcode.py
    commandcode.py
  tests/
```

## M1.2 State store

Maintain one immutable/latest in-memory snapshot containing:

- normalized ZCode tasks,
- normalized CommandCode usage,
- source timestamps,
- source error state,
- server timestamp/version.

## M1.3 Collectors

Initial cadence:

- ZCode: detect/read every 1–2 seconds.
- CommandCode: refresh every 30–60 seconds.
- Backend health: update every 5 seconds.

These are defaults, not API guarantees.

## M1.4 HTTP API

Initial endpoints:

- `GET /api/v1/state`
- `GET /api/v1/health`

For v1 the Android client will poll `/state`; streaming is deferred until measurement proves polling inadequate.

## M1.5 Resilience

Required behavior:

- One failed adapter must not take down the service.
- Last known good state may be retained but must be marked stale.
- Errors are represented explicitly; never silently turn adapter failure into an empty task list or zero usage.

### Exit criteria

A curl request returns a complete canonical state snapshot while both adapters can independently be mocked or disconnected.

---

# Milestone 2 — Native Android MVP

## M2.1 Minimal project

Use:

- Java
- XML layouts
- Android framework Views
- `HttpURLConnection` unless testing shows a concrete need for an HTTP dependency
- `org.json` for the initial parser
- `SharedPreferences` for server configuration

Avoid WebView, Compose, Flutter, React Native, and heavyweight UI libraries.

## M2.2 Setup screen

First-run configuration:

- server URL,
- refresh interval,
- connectivity test.

Persist locally.

## M2.3 Dashboard screen

Display:

- LIVE / STALE / OFFLINE indicator,
- ZCode summary counts,
- running/waiting/failed task cards,
- CommandCode plan,
- remaining quota/credit,
- rolling usage windows,
- reset countdowns,
- last successful refresh.

## M2.4 Old-device behavior

Requirements:

- Keep screen on while HUD is visible.
- No full-screen re-inflation on each refresh.
- Update only changed text/progress values.
- Avoid bitmap-heavy assets and unnecessary animation.
- Handle Wi-Fi loss/reconnect without user action.
- Use conservative timeouts.

### Exit criteria

The target 2018 Android device can run the HUD continuously with acceptable responsiveness and no browser/WebView process.

---

# Milestone 3 — CI build pipeline

## M3.1 GitHub Actions

On push/PR:

- set up JDK,
- validate Gradle wrapper,
- build Android debug APK,
- run unit tests,
- upload APK artifact.

## M3.2 Release build

Later, add a manually triggered/tagged release workflow using a persistent signing key stored only in GitHub Secrets.

Secrets must never be committed.

### Exit criteria

A clean checkout can produce an installable APK entirely in GitHub Actions.

---

# Milestone 4 — Integration and hardening

- Test against actual ZCode upgrades/schema differences.
- Test CommandCode auth expiration and quota reset transitions.
- Test backend restart while Android remains open.
- Test Android network switching and extended offline periods.
- Add schema/version negotiation if needed.
- Measure Android memory/CPU/battery/temperature behavior.
- Add kiosk-like immersive mode only after baseline stability is confirmed.

---

# Milestone 5 — v1 release

v1 is complete when:

- ZCode task state is trustworthy and visibly stale when not trustworthy.
- CommandCode usage is trustworthy and visibly stale when not trustworthy.
- Android installs from a CI-built APK.
- No credentials are embedded in the APK or repository.
- Target phone can be left running as a dedicated display.
- Setup/recovery steps are documented.

---

# Work order

Priority order:

1. Source discovery.
2. Canonical API contract.
3. Backend mocks and state service.
4. Android shell against mock API.
5. Real ZCode adapter.
6. Real CommandCode adapter.
7. CI APK build.
8. Device integration/performance pass.
9. Signing/release.

This order intentionally allows Android development to proceed against mock JSON before vendor adapters are finalized.
