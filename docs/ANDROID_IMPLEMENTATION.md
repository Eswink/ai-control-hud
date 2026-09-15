# Android implementation

The Android client is deliberately conservative for an older device.

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

Usage reset countdowns are rendered locally once per second from `resetAt`; this does not cause additional backend requests.

## First-run connection

The setup panel accepts a backend address. If no scheme is supplied, `http://` is assumed for the trusted-LAN v1 deployment. The app calls `/api/v1/health` before persisting the address.

Cleartext traffic is explicitly enabled because LAN HTTP is part of the current v1 scope. Public Internet exposure remains out of scope.

## HUD rendering

The dashboard renders:

- aggregate LIVE / DEGRADED / OFFLINE state;
- latest successful snapshot time;
- independent ZCode and CommandCode source health, including sanitized source messages;
- ZCode running/waiting/failed summary counters;
- up to four task cards, ordered failed -> running -> waiting -> unknown -> completed;
- task workspace, duration, current activity, and additions/deletions when available;
- an overflow count when more than four tasks exist;
- CommandCode plan, credit balance, 5h and weekly usage windows;
- local reset countdowns and usage progress indicators.

Task rows are created only as needed and then reused. Normal two-second refreshes mutate existing views instead of rebuilding the dashboard hierarchy. Decorative animations and bitmap-heavy assets are intentionally absent.

Usage indicators use semantic thresholds: normal below 70%, warning from 70%, and high usage from 90%. Failed tasks and source errors receive the strongest visual warning.

## Remaining device validation

The code-level old-device constraints are enforced in CI with `minSdk 23` and Android lint. Real-device profiling still belongs to issue 10 and must validate responsiveness, memory use, temperature, screen-on behavior, Wi-Fi recovery, and long-running foreground operation on the target phone.

## CI

`.github/workflows/android.yml` installs the Android SDK and a fixed Gradle version in GitHub Actions, then runs lint, unit-test tasks and `assembleDebug`. The APK is uploaded as `ai-control-hud-debug-apk`.

No Android SDK or Android Studio is required on the developer machine.
