# Go Agent source diagnostics

Status: operational diagnostics contract with R3 durable-outbox metadata

The development-machine Agent exposes a local/trusted-LAN diagnostics endpoint without changing the Android-facing schema-v1 state contract:

```text
GET /api/v1/diagnostics
```

This endpoint is intended for operator troubleshooting and the future native Windows Agent UI. It reports bounded operational metadata without exposing credentials, provider configuration, private filesystem paths, task content, workspace names, event JSON, or raw source error text.

## Response contract

Diagnostics has its own version independent from state schema v1. `outbox` is an additive optional field in diagnostics v1 and is present only when remote Hub upload (and therefore the durable event outbox) is configured.

```json
{
  "diagnosticsVersion": 1,
  "stateSchemaVersion": 1,
  "role": "agent",
  "version": "0.3.0-go-dev",
  "time": "2026-09-17T00:00:00Z",
  "uptimeSeconds": 120,
  "sources": {
    "zcode": {
      "enabled": true,
      "adapterKind": "zcode.sqlite",
      "status": "ok",
      "observedAgeSeconds": 0,
      "lastSuccessAgeSeconds": 0,
      "schemaSupport": "supported"
    },
    "commandCode": {
      "enabled": true,
      "adapterKind": "commandcode.provider-http",
      "status": "stale",
      "observedAgeSeconds": 8,
      "lastSuccessAgeSeconds": 68,
      "schemaSupport": "supported"
    }
  },
  "zcodeStorage": {
    "bindingMode": "machine-config",
    "layoutSource": "machine-config",
    "runtimeDatabaseReadable": true,
    "taskIndexReadable": true,
    "turnLogReadable": true,
    "refreshRecommended": false
  },
  "outbox": {
    "status": "ok",
    "pendingEvents": 3,
    "taskBaselineRows": 1250,
    "compactedTaskRows": 900,
    "oldestPendingAgeSeconds": 90,
    "reusableBytes": 32768
  }
}
```

All values above are synthetic examples.

## Source fields

`enabled` is false only when the canonical source health is `disabled`/not configured.

`adapterKind` is a fixed implementation label, not a provider URL or local path:

- `zcode.sqlite`
- `commandcode.provider-http`

`status` is the canonical source-health status: `ok`, `stale`, `error`, or `disabled`.

`observedAgeSeconds` is the non-negative age of the most recent source-health evaluation relative to the Agent's current clock.

`lastSuccessAgeSeconds` is present only after a trustworthy successful collection has occurred. It is `null` before the first success or for a never-successful error/disabled source.

`schemaSupport` is one of:

- `supported` — the source has produced at least one trustworthy successful snapshot;
- `unsupported` — the current sanitized source failure is an explicit unsupported-schema/response failure;
- `unknown` — the source is enabled but has not yet produced a success and is failing/waiting for a reason that does not establish schema compatibility;
- `not-configured` — the source is disabled/not configured.

## ZCode storage binding fields

The optional `zcodeStorage` object is deliberately path-free:

- `bindingMode`: `foreground` when the Agent resolved the current user's layout directly, or `machine-config` for a service using persisted source bindings;
- `layoutSource`: a fixed enum-like label such as `default`, `data_base_setting`, `data_base_env`, `zcode_home`, `hud_zcode_home`, or `machine-config`;
- `runtimeDatabaseReadable`: the bound runtime SQLite file can be opened read-only;
- `taskIndexReadable`: the bound task-index SQLite file can be opened read-only;
- `turnLogReadable`: the bound CLI log directory can be opened/read;
- `refreshRecommended`: at least one configured database source is no longer readable.

No source path is returned. For an installed Windows service, `refreshRecommended=true` is an operator signal to run `ai-control-agent.exe service refresh-zcode` from the interactive user's elevated terminal. The service does not attempt to infer that user's profile from LocalSystem.

The interactive `doctor` command separately compares the current user's resolved ZCode layout with the persisted service binding and prints only `binding=current|different` plus the sanitized layout-source label. It does not send that comparison over HTTP.

## Durable outbox fields

The optional `outbox` object contains only aggregate metadata:

- `status`: `ok`, `warning`, `error`, or `not-configured`;
- `pendingEvents`: durable terminal events not yet acknowledged by the Hub;
- `taskBaselineRows`: remembered task identities/statuses used to avoid replaying old terminal transitions;
- `compactedTaskRows`: old terminal baselines whose display payload has been cleared while identity/status is retained;
- `oldestPendingAgeSeconds`: age of the oldest pending durable event, or null when none are pending;
- `reusableBytes`: SQLite free-page bytes available for reuse.

`warning` is currently used when pending events reach 10,000 or the oldest pending event is at least 24 hours old. The UI should display the warning, not delete data.

R3 maintenance never deletes pending `event_outbox` rows. Terminal baseline compaction keeps `task_id`, status, and last-seen time so re-observing the same historical completed/failed task does not create a duplicate semantic event.

## Security boundary

The diagnostics response deliberately omits:

- CommandCode API keys or authorization headers;
- Hub bearer tokens;
- DPAPI/SecretStore contents;
- CommandCode provider URL/base URL;
- ZCode/outbox database paths;
- user profile paths;
- task titles/workspaces/activity;
- event IDs/event JSON;
- credit/plan/usage payloads;
- raw source error messages.

The existing canonical `/api/v1/state` health message remains independently sanitized by the runtime. Diagnostics does not copy source free-text messages into the operational metadata.

## Relationship to other APIs

`GET /api/v1/state` remains schema-v1 and is unchanged.

`GET /api/v1/health` remains the small reachability/source-status response.

`GET /api/v1/diagnostics` is richer operational metadata for the **development-machine Agent**. It is not required by Android and is not an Agent-to-Hub transport contract.

The Central Hub does not need this endpoint to ingest state/events.

## CI coverage

Go API tests cover healthy, disabled, unsupported-schema, and optional outbox diagnostics and assert that private source error text is absent from the encoded response.

Events tests cover durable pending preservation, terminal-baseline payload compaction, duplicate-event prevention after compaction, and the 1,000-row compaction batch bound.

The native Agent runtime/service matrix continues to run on Windows, Linux, macOS arm64, and macOS amd64.
