# Command Code source strategy

## Deployment architecture

The target machine does **not** consume Command Code through the Command Code CLI. Command Code is configured as a third-party model provider inside ZCode.

```text
ZCode
  -> ~/.zcode/v2/config.json provider entry
  -> OpenAI/Anthropic-compatible Command Code endpoint
  -> Command Code plan/credits
```

ZCode's current provider configuration recognizes connection fields such as `options.apiKey`, `options.baseURL`, `options.apiKeyRequired`, and `options.headers`. The API key remains local to the development PC.

## Provider detection

`python -m tools.provider_discovery` produces a secret-free view of the ZCode provider config. It exposes only:

- provider id/name;
- enabled state;
- protocol/kind when present;
- base URL without query/fragment/userinfo;
- host;
- API-key presence boolean;
- header names only;
- model ids;
- whether the host is the official `api.commandcode.ai` endpoint.

It never writes API-key values or header values.

The production adapter auto-enables only when the provider host is exactly `api.commandcode.ai`. A proxy/custom endpoint must be selected explicitly with `HUD_COMMANDCODE_PROVIDER_ID` after manual verification.

## Billing retrieval

Command Code's documented Provider API is the supported model-traffic surface. The usage dashboard still requires plan/credit/window data that is not part of the public Provider API contract.

Current community evidence (August/September 2026) shows API-key access to:

- `GET https://api.commandcode.ai/alpha/billing/credits`
- `GET https://api.commandcode.ai/alpha/billing/subscriptions`

These paths are treated as **unstable implementation details**. They exist only inside `CommandCodeZCodeProviderAdapter`; the Android/canonical API does not know their paths or raw payloads.

Credits are the required source. Subscription lookup is best-effort enrichment so a subscription endpoint outage does not discard valid credit/window data.

## Normalized mapping

Current adapter mapping:

- `credits.monthlyCredits + max(credits.purchasedCredits, 0)` -> `credit.remaining`;
- credit unit -> `USD`;
- `windowLimits.fiveHour.used / cap` -> `5h.usedPercent`;
- `windowLimits.fiveHour.resetAt` -> `5h.resetAt`;
- `windowLimits.weekly.used / cap` -> `weekly.usedPercent`;
- `windowLimits.weekly.resetAt` -> `weekly.resetAt`;
- successful subscription `data.planId` -> `plan`.

No monthly `credit.limit` is fabricated because the billing payload/plan catalog semantics have not yet been verified on the target account.

## Failure semantics

- provider missing -> source disabled until a provider is explicitly selected or detected;
- provider exists but key missing -> explicit error;
- 401 credits response -> authentication failed;
- 403 credits response -> billing access denied;
- other non-2xx credits response -> billing API unavailable;
- malformed/changed credits schema -> explicit unsupported response error;
- later collection failure after a successful snapshot -> runtime preserves last-known-good data as `stale`.

No failure is converted to zero credits or zero usage.

## Target-machine verification

Run:

```cmd
python -m tools.provider_discovery
```

Review and share `.local\provider-discovery.json`. If the provider host is `api.commandcode.ai`, normal backend startup can auto-enable the adapter. If the provider is a local proxy instead, verify that its configured key is still a Command Code API key before setting `HUD_COMMANDCODE_PROVIDER_ID`.

References:

- ZCode model/provider configuration: https://zcode.z.ai/en/docs/configuration
- Command Code Provider API: https://commandcode.ai/docs/provider
- Command Code Studio/API keys: https://commandcode.ai/docs/studio
