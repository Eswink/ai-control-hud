# AI Control HUD

A lightweight always-on control panel for monitoring local AI coding agents from an old Android device.

## Goals

- Show ZCode task state with low latency.
- Show CommandCode plan/usage/remaining quota and reset windows.
- Run the data collectors on the development machine using Python.
- Run a native Android client optimized for older devices.
- Require no local Android build environment; APKs are built by GitHub Actions.
- Keep credentials and vendor-specific implementation details off the Android device.

## Target architecture

```text
ZCode local data ----\
                      > Python state service ---- JSON/HTTP ---- Native Android HUD
CommandCode usage ---/                         \---- health ----/

GitHub Actions ---- build/sign APK ---- release artifact
```

## Technology constraints

### Server

- Python 3
- Standard library where practical
- FastAPI/HTTP layer only if it materially simplifies deployment
- SQLite/log adapters isolated behind stable interfaces

### Android

- Native Android
- Java + XML Views
- No WebView
- No Compose
- No Flutter/React Native
- Minimize third-party dependencies
- `minSdk` target: API 23 unless device verification requires otherwise

### Build

- GitHub Actions
- Gradle wrapper committed to the repository
- No Android Studio required locally

## Current status

Planning and source-discovery phase. Vendor internals are intentionally treated as unverified until inspected on the actual development machine.

See:

- [`docs/PLAN.md`](docs/PLAN.md)
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/API.md`](docs/API.md)
- [`docs/DECISIONS.md`](docs/DECISIONS.md)
