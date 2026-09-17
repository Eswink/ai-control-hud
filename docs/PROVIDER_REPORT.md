# Provider-discovery report compatibility

`tools.provider_discovery` emits a privacy-safe evidence report used while verifying ZCode/CommandCode provider configuration. Its `reportVersion` is a separate contract from `tools.discovery` even when both currently use the numeric value `2`.

## Current contract

```text
current provider reportVersion: 2
supported consumer versions: 1, 2
```

Consumers must use `tools.provider_report` rather than guessing fields from an arbitrary JSON object.

Validate a saved report with:

```text
python -m tools.provider_report .local/provider-discovery.json
```

Exit codes:

- `0`: supported report;
- `2`: malformed or unsupported report.

## Version 1

The original provider report contained:

- `reportVersion=1`;
- one `zcodeProviderConfig` object;
- privacy metadata.

It predates multi-config discovery, the explicit `commandCodeCandidates` section, and runtime SQLite provider/model evidence.

The compatibility loader therefore performs only structural normalization:

- wraps `zcodeProviderConfig` as `zcodeProviderConfigs=[...]`;
- adds missing privacy guarantees for setting values and session IDs as `false`;
- sets `commandCodeCandidates=null`;
- sets `runtimeDbEvidence=null`.

`null` is intentional. A v1 report did not contain enough evidence to reconstruct those later semantic sections, so the loader does not infer them.

## Version 2

Version 2 is the current generator contract. It requires:

- `zcodeProviderConfigs` array;
- `commandCodeCandidates` array;
- `runtimeDbEvidence` object or `null`;
- privacy metadata object.

Later privacy-safe evidence fields may be added compatibly while staying on version 2 when their absence has an unambiguous optional meaning.

## Future versions

Unknown versions such as `3` are rejected with `UnsupportedProviderReportVersion`. Consumers must not silently treat a future report as v2.

The test suite also asserts that `tools.provider_discovery.build_report()` emits the same version declared by `CURRENT_PROVIDER_REPORT_VERSION`. A future generator version bump must therefore update the loader and compatibility documentation in the same change.

## Security rule

Compatibility normalization must never reconstruct secrets, provider credentials, header values, prompt/message contents, session IDs, or setting values. It deep-copies input reports and only adds metadata that is safe to infer structurally.
