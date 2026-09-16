# Go Agent source diagnostics

Status: H11 operational diagnostics contract

The Windows development-machine Agent exposes a local/trusted-LAN diagnostics endpoint without changing the Android-facing schema-v1 state contract:

```text
GET /api/v1/diagnostics
```

This endpoint is intended for operator troubleshooting. It reports source-adapter state without exposing credentials, provider configuration, private filesystem paths, task content, workspace names, or raw source error text.

## Response contract

Diagnostics has its own version independent from state schema v1:

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

## Security boundary

The diagnostics response deliberately omits:

- CommandCode API keys or authorization headers;
- Hub bearer tokens;
- DPAPI/SecretStore contents;
- CommandCode provider URL/base URL;
- ZCode database paths;
- user profile paths;
- task titles/workspaces/activity;
- credit/plan/usage payloads;
- raw source error messages.

The existing canonical `/api/v1/state` health message remains independently sanitized by the runtime. Diagnostics does not copy that free-text message at all, which prevents a future collector error from accidentally turning this operational endpoint into a path/log disclosure surface.

## Relationship to other APIs

`GET /api/v1/state` remains schema-v1 and is unchanged.

`GET /api/v1/health` remains the small reachability/source-status response.

`GET /api/v1/diagnostics` is richer operational metadata for the **development-machine Agent**. It is not required by Android and is not an Agent-to-Hub transport contract.

The Central Hub does not need this endpoint to ingest state/events, and H11 does not add a new upload payload or credential.

## CI coverage

Go API tests cover healthy, disabled, and explicit unsupported-schema diagnostics and assert that private source error text is absent from the encoded response.

The native Agent runtime smoke runs on Windows, Linux, macOS arm64, and macOS amd64. It queries `/api/v1/diagnostics`, validates adapter/status/schema fields, and verifies that its temporary private database path is absent from the response.
