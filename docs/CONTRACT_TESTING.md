# Schema-v1 contract testing

The canonical state contract is intentionally shared by the Windows Go Agent, Go Central Hub, and Android HUD. CI uses one set of sanitized JSON fixtures rather than maintaining independent copies for each implementation.

## Shared fixtures

Canonical schema-v1 examples live under:

```text
server/tests/fixtures/
```

Current fixtures cover:

- `healthy.json`
- `zcode_stale.json`
- `commandcode_auth_error.json`
- `backend_degraded.json`
- `task_failed.json`

They contain synthetic data only.

## Go coverage

Go domain/API tests load these fixtures through the production schema-v1 model and validate canonical health/data invariants. This protects Agent/Hub serialization and the rule that error/disabled sources cannot masquerade as trustworthy empty/zero data.

## Android coverage

`android/app/build.gradle` exposes the same directory as JVM test resources. `StateSnapshotContractTest` executes the production Android `StateSnapshot.parse()` implementation against every canonical fixture.

The Android gate verifies:

- all current schema-v1 health/data combinations parse;
- source and overall health values survive parsing;
- nullable task/usage sections preserve their semantics;
- healthy dashboard fields such as activity, duration, additions/deletions and usage windows remain readable;
- unknown optional fields are ignored for forward-compatible schema-v1 expansion;
- an unsupported future `schemaVersion` is rejected explicitly.

The JVM test classpath includes `org.json` only as a **test dependency** so the production parser can run outside an Android device. Android runtime behavior continues to use the platform-provided `org.json` implementation; the test dependency is not packaged into the APK.

## Change rule

When schema-v1 gains a backward-compatible optional field:

1. update production model/parser behavior as needed;
2. update or add a sanitized canonical fixture when the new field needs cross-implementation coverage;
3. keep existing fixtures valid;
4. ensure both Go and Android tests remain green.

Removing or renaming existing fields, changing nullability/health semantics, or changing the meaning of an existing value is not an optional schema-v1 extension and requires an explicit schema-version decision.
