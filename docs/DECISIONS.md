# Architecture Decision Record

This file records the initial engineering decisions for v1. Change decisions when evidence requires it; do not silently drift.

## ADR-001 — Native Android client

**Decision:** use native Android Java + XML Views.

**Reasoning:** the target is an older Android device and the product is a dedicated always-on dashboard. Avoiding WebView/browser rendering and cross-platform runtimes reduces memory, CPU, and operational complexity.

**Rejected for v1:** PWA/WebView, Flutter, React Native, Compose.

---

## ADR-002 — Cloud Android builds

**Decision:** build APKs with GitHub Actions; local Android tooling is not required.

**Reasoning:** the development machine has Python and Go available but no Android toolchain. CI should be the reproducible build authority.

---

## ADR-003 — Python owns vendor integrations

**Decision:** ZCode and CommandCode integrations run only on the development machine.

**Reasoning:** local files/auth are naturally available there, credentials stay off the phone, and vendor changes remain isolated from the Android binary.

---

## ADR-004 — Polling before streaming

**Decision:** Android v1 polls a cached `/api/v1/state` snapshot, initially every 2 seconds.

**Reasoning:** predictable lifecycle/reconnect behavior is more valuable than transport sophistication on the target old device. The backend performs vendor collection independently, so polling does not multiply vendor load.

**Revisit when:** measured network/CPU cost is unacceptable or sub-second push becomes a real requirement.

---

## ADR-005 — Canonical vendor-neutral API

**Decision:** Android never consumes raw ZCode or CommandCode payloads.

**Reasoning:** both integrations may rely on local/undocumented implementation details that can change. A canonical DTO localizes breakage to one adapter.

---

## ADR-006 — Explicit stale/error semantics

**Decision:** a collector failure cannot be represented as valid empty/zero data.

**Reasoning:** an always-on status display is dangerous if it looks healthy while its source is broken. Last-known-good values must carry freshness metadata.

---

## ADR-007 — Minimal Android dependencies

**Decision:** start with Android framework APIs (`HttpURLConnection`, `org.json`, XML/View widgets, `SharedPreferences`).

**Reasoning:** dependency cost must be justified by measured complexity or reliability benefit.

**Revisit when:** implementation becomes materially safer or simpler with a small, well-supported dependency.

---

## ADR-008 — No public exposure in v1

**Decision:** assume trusted LAN or private overlay networking.

**Reasoning:** the dashboard contains operational development metadata and may be backed by local authenticated integrations. Direct public exposure creates unnecessary security scope.

---

## ADR-009 — `minSdk` starts at API 23

**Decision:** plan for Android 6.0+ unless the target phone requires a different lower bound.

**Reasoning:** API 23 provides broad legacy-device coverage while keeping implementation manageable. Final value is verified against the actual device before Android implementation is frozen.

---

## ADR-010 — Secrets never enter source control

**Decision:** repository examples are sanitized; local auth, databases, logs, keystores, signing passwords and environment files are excluded.

**Reasoning:** the repository may be public and CI signing requires persistent credentials. Secrets belong in local protected storage or GitHub Secrets, not commits.
