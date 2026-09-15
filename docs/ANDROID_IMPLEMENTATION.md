# Android implementation

The first Android client is deliberately conservative for an older device.

## Runtime stack

- Java only;
- platform XML/View widgets;
- `HttpURLConnection`;
- `org.json`;
- `SharedPreferences`;
- one foreground Activity;
- one single-thread network executor;
- no WebView, Compose, Flutter, React Native, Retrofit, OkHttp, or image framework.

`minSdk` is API 23. The CI build uses JDK 17, AGP 8.9.3, Gradle 8.11.1 and Android API 35.

## Lifecycle and polling

The Activity keeps the screen on while foregrounded. It polls `/api/v1/state` every two seconds after a successful response. Only one request may be in flight. Failures use exponential backoff capped at 30 seconds. Returning to the Activity restarts polling; leaving it stops scheduling new polls.

An unsupported `schemaVersion` stops polling and presents a compatibility error rather than attempting to parse unknown semantics.

## First-run connection

The setup panel accepts a backend address. If no scheme is supplied, `http://` is assumed for the trusted-LAN v1 deployment. The app calls `/api/v1/health` before persisting the address.

Cleartext traffic is explicitly enabled because LAN HTTP is part of the current v1 scope. Public Internet exposure remains out of scope.

## UI scope

This shell renders:

- aggregate LIVE / DEGRADED / OFFLINE state;
- ZCode source health and summary counters;
- the first task as a proof of canonical task rendering;
- CommandCode health, plan, credit and 5h/weekly windows.

The full bounded task list, stale-age display, countdown formatting and old-device profiling belong to #8/#10.

## CI

`.github/workflows/android.yml` installs the Android SDK and a fixed Gradle version in GitHub Actions, then runs lint, unit-test tasks and `assembleDebug`. The APK is uploaded as `ai-control-hud-debug-apk`.

No Android SDK or Android Studio is required on the developer machine.
