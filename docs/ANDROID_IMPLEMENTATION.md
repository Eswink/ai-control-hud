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

The Activity keeps the screen on while foregrounded. It polls `/api/v1/state` every two seconds after a successful response. Only one request may be in flight. Transport failures and server HTTP errors use exponential backoff capped at 30 seconds. Returning to the Activity restarts polling; leaving it stops scheduling new polls.

An unsupported `schemaVersion` stops polling and presents a compatibility error rather than attempting to parse unknown semantics.

Usage reset countdowns are rendered locally once per second from `resetAt`; this does not cause additional backend requests.

## Connection-state semantics

The dashboard deliberately separates network reachability from Agent availability:

- `CONNECTING` — no state response has been classified yet;
- `WAITING FOR AGENT` — the Hub answered `/api/v1/state` with HTTP 503, which means the Hub is online but has no primary-Agent snapshot yet;
- `LIVE` — a valid schema-v1 snapshot reports aggregate live state;
- `DEGRADED` — a valid snapshot reports stale/error source health;
- `SERVER ERROR` — the Hub is reachable but returned another non-2xx HTTP status;
- `OFFLINE` — the HTTP request failed at the transport/IO layer;
- `SCHEMA ERROR` — the backend schema is incompatible with the APK.

A valid HTTP error response proves that the current physical Hub address is reachable. In `auto.lan` mode such responses therefore do **not** invalidate the discovered address or trigger UDP rediscovery. Only transport/IO failures trigger rediscovery.

`WAITING FOR AGENT` is polled at the normal two-second interval rather than entering failure backoff. The UI clears any previously rendered snapshot values and shows unavailable placeholders so a reset/new Hub cannot accidentally display an old in-memory snapshot as current data.

## First-run connection

The setup panel accepts a backend address. If no scheme is supplied, `http://` is assumed for the trusted-LAN v1 deployment. The app calls `/api/v1/health` before persisting the address.

This means a healthy Hub can be saved before Windows has uploaded its first snapshot: `/health` may already be HTTP 200 while `/state` is HTTP 503. The dashboard then shows `WAITING FOR AGENT` until the first snapshot arrives.

Cleartext traffic is explicitly enabled because LAN HTTP is part of the current v1 scope. Public Internet exposure remains out of scope.

## HUD rendering

The dashboard renders:

- aggregate CONNECTING / WAITING / LIVE / DEGRADED / SERVER ERROR / OFFLINE state;
- latest successful snapshot time or explicit waiting message;
- independent ZCode and CommandCode source health, including sanitized source messages;
- ZCode running/waiting/failed summary counters;
- up to four task cards, ordered failed -> running -> waiting -> unknown -> completed;
- task workspace, duration, current activity, and additions/deletions when available;
- an overflow count when more than four tasks exist;
- CommandCode plan, credit balance, 5h and weekly usage windows;
- local reset countdowns and usage progress indicators.

Task rows are created only as needed and then reused. Normal two-second refreshes mutate existing views instead of rebuilding the dashboard hierarchy. Decorative animations and bitmap-heavy assets are intentionally absent.

Usage indicators use semantic thresholds: normal below 70%, warning from 70%, and high usage from 90%. Failed tasks and source errors receive the strongest visual warning.

## Auto-discovery interaction

In automatic mode the configured identity stays `http://auto.lan`, while the latest UDP-resolved physical address is displayed as `AUTO · http://<ip>:8787`.

- connection/IO failure invalidates the cached physical address and runs UDP discovery again;
- HTTP 503 waiting state keeps the current physical address because the Hub just proved it is reachable;
- other HTTP errors also keep the current physical address and are rendered as server errors rather than network-offline state.

The stable logical identity remains unchanged, so physical-address changes do not reset the event cursor.

## Voice/event behavior

Task events remain independent from snapshot status. Android stores its event cursor in `SharedPreferences`, establishes a silent first baseline, silently rebases after a lower Hub high-water mark, and uses local TextToSpeech for eligible completed/failed events.

Completed and failed speech are independently configurable. Quiet hours default to 23:00–08:00. Old backlog is not spoken item-by-item; at most one catch-up summary is emitted outside quiet hours.

## Validation policy

Android behavior is gated in CI by `minSdk 23`, lint, JVM unit tests, and debug APK assembly. The real-device core path (auto-discovery, dashboard and TTS) was already accepted during the Central Hub cutover. Subsequent UI/state-semantics iterations do not require additional operator field testing unless explicitly requested.

## CI

`.github/workflows/android.yml` installs the Android SDK and a fixed Gradle version in GitHub Actions, then runs lint, unit-test tasks and `assembleDebug`. The APK is uploaded as `ai-control-hud-debug-apk`.

`StateClientTest` specifically protects the H10 reachability contract: state HTTP 503 maps to waiting-for-Agent, HTTP responses do not trigger LAN rediscovery, and transport failures still do.

No Android SDK or Android Studio is required on the developer machine.
