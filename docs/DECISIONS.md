# Architecture Decision Record

This file records engineering decisions and their evolution. Superseded decisions remain visible so architecture changes are explicit rather than silent drift.

## ADR-001 — Native Android client

**Decision:** use native Android Java + XML Views.

**Reasoning:** the target is an older Android device and the product is a dedicated always-on dashboard. Avoiding WebView/browser rendering and cross-platform runtimes reduces memory, CPU, and operational complexity.

**Rejected for v1:** PWA/WebView, Flutter, React Native, Compose.

---

## ADR-002 — Cloud Android builds

**Decision:** build APKs with GitHub Actions; local Android tooling is not required.

**Reasoning:** CI should be the reproducible build authority and the development machine does not need an Android toolchain.

---

## ADR-003 — Vendor integrations stay on the development machine

**Status:** amended.

**Original decision:** Python owns ZCode and CommandCode integrations on the development machine.

**Current decision:** vendor integrations still run only on the development machine, but the production implementation is the Go `ai-control-agent`. The legacy Python adapters remain reference/test implementations.

**Reasoning:** local files/auth are naturally available on the development machine, vendor credentials stay off the Linux hub and Android device, and vendor-specific changes remain isolated from downstream clients.

---

## ADR-004 — Polling before streaming

**Decision:** Android v1 polls a cached `/api/v1/state` snapshot.

**Reasoning:** predictable lifecycle/reconnect behavior is more valuable than transport sophistication on the target old device. Backend collection is independent, so Android polling does not multiply vendor load.

**Amendment:** Central Hub V2 keeps snapshot polling during the migration. Durable events are a separate protocol. Long polling or another event transport may be added later if measurements justify it.

---

## ADR-005 — Canonical vendor-neutral API

**Decision:** Android and the Linux hub never consume raw ZCode or CommandCode payloads.

**Reasoning:** both integrations may depend on local/undocumented implementation details that can change. A canonical DTO localizes breakage to the development-machine collector boundary.

---

## ADR-006 — Explicit stale/error semantics

**Decision:** a collector or connectivity failure cannot be represented as valid empty/zero data.

**Reasoning:** an always-on status display is unsafe if it looks healthy while its source is broken. Last-known-good values must carry freshness metadata.

**Amendment:** hub-level heartbeat expiry may downgrade agent-reported `ok` data to `stale`, but the hub must never upgrade source health or fabricate fresh values.

---

## ADR-007 — Minimal Android dependencies

**Decision:** start with Android framework APIs (`HttpURLConnection`, `org.json`, XML/View widgets, `SharedPreferences`).

**Reasoning:** dependency cost must be justified by measured complexity or reliability benefit.

**Revisit when:** implementation becomes materially safer or simpler with a small, well-supported dependency.

---

## ADR-008 — No public exposure by default

**Decision:** assume trusted LAN or private overlay networking.

**Reasoning:** the dashboard contains operational development metadata and agent ingest requires authentication. Direct public exposure creates unnecessary security scope.

**Amendment:** Central Hub V2 does not change this boundary. Use a private overlay and/or TLS rather than opening the hub directly to the public Internet.

---

## ADR-009 — `minSdk` starts at API 23

**Decision:** target Android 6.0+ unless the actual device requires a different bound.

**Reasoning:** API 23 provides broad legacy-device coverage while keeping implementation manageable.

---

## ADR-010 — Secrets never enter source control

**Decision:** repository examples are sanitized; local auth, databases, logs, keystores, signing passwords, deployment tokens, and environment files are excluded.

**Reasoning:** the repository is public and persistent credentials belong in protected runtime storage or CI secret stores, not commits.

---

## ADR-011 — A 24/7 Linux hub becomes the Android authority

**Decision:** Android will read state from an always-on Linux hub instead of depending directly on the development machine being powered on.

**Reasoning:** the development machine is not continuously available. The hub can retain the last trustworthy state, explicitly represent machine-offline freshness, and later host durable notification events.

**Constraint:** the development machine remains the authority for vendor collection; the hub is an authority for relay/persistence/freshness, not vendor credentials.

---

## ADR-012 — Agent push, never hub pull

**Decision:** the development-machine agent initiates outbound state and heartbeat requests to the hub. The hub does not poll Windows.

**Reasoning:** outbound-only transport works across ordinary NAT/firewall configurations, avoids opening a new Windows inbound service for the hub, and naturally makes heartbeat absence a machine-offline signal.

---

## ADR-013 — Keep the local Go HTTP API

**Decision:** `/api/v1/state` and `/api/v1/health` remain available locally on the Go agent even after the hub becomes the Android authority.

**Reasoning:** local endpoints provide a clean diagnostic boundary. An operator can distinguish collector failure from hub/upload failure without involving Android or the network relay.

---

## ADR-014 — Snapshot and event protocols are distinct

**Decision:** snapshots represent current state; durable events represent transitions that must not be lost, such as task completion/failure.

**Reasoning:** inferring notifications only from Android snapshot comparisons can miss transitions during app/network downtime and can replay old transitions after reconnect. Durable events with stable IDs and server sequence cursors provide explicit delivery semantics.

**Target semantics:** at-least-once agent delivery, idempotent hub insertion, ordered Android consumption. Exactly-once delivery is not required.

---

## ADR-015 — SQLite before distributed infrastructure

**Decision:** the initial Linux hub uses SQLite for agent metadata, latest snapshots, and the future event log.

**Reasoning:** this is a single-user, low-throughput deployment. Redis, PostgreSQL, Kafka, RabbitMQ, MQTT, and Kubernetes would add operational state without solving a measured bottleneck.

**Revisit when:** concurrency, history volume, multi-user requirements, or deployment topology demonstrably exceed SQLite's role.

---

## ADR-016 — Notification policy belongs to Android

**Decision:** the hub emits semantic events such as `task.completed`; Android decides whether and how to interrupt the user.

**Reasoning:** quiet hours, speech enablement, event age, device volume, and foreground/background behavior are device/user policy rather than server truth.

**Planned default:** completion/failure speech outside 23:00–08:00 quiet hours; stale historical events are not spoken individually.

---

## ADR-017 — Foreground HUD first; background push is deferred

**Decision:** retain the dedicated foreground-HUD lifecycle as the primary product mode during Central Hub V2.

**Reasoning:** the old phone is intended to be plugged in and used as an always-on display. Adding FCM/background delivery before real-device evidence would introduce another credential/service/lifecycle surface unnecessarily.

**Revisit when:** screen-off or background completion alerts become a demonstrated requirement.
