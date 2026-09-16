# Android release signing

The normal pull-request Android build does not require or receive release-signing secrets. Production-signed release APKs are produced only by `.github/workflows/android-release.yml` on a `v*` tag or an explicit `workflow_dispatch` run.

## Required GitHub Secrets

Configure these repository/environment secrets before running a production-signed release:

```text
HUD_ANDROID_KEYSTORE_BASE64
HUD_ANDROID_KEYSTORE_PASSWORD
HUD_ANDROID_KEY_ALIAS
HUD_ANDROID_KEY_PASSWORD
```

`HUD_ANDROID_KEYSTORE_BASE64` is the complete release keystore encoded as base64. The keystore file itself must never be committed.

The repository `.gitignore` excludes common keystore/private-key extensions including `.jks`, `.keystore`, `.p12`, `.pfx`, `.pem`, and `.key`.

## Version metadata

Development/ordinary CI builds default to:

```text
versionName=0.1.0-dev
versionCode=1
```

Release metadata is injected through:

```text
HUD_ANDROID_VERSION_NAME
HUD_ANDROID_VERSION_CODE
```

Gradle validates both values. `versionName` accepts 1–100 safe alphanumeric/version punctuation characters; `versionCode` must be an integer in `1..2100000000`.

For production release workflow runs:

- tag `v1.2.3` becomes `versionName=1.2.3`;
- manual `workflow_dispatch` uses its required `version_name` input;
- `versionCode` is the Android Release workflow's monotonic `GITHUB_RUN_NUMBER`.

After building, CI uses `aapt dump badging` to read the APK manifest and prove that the requested `versionName` and `versionCode` were actually embedded. The release artifact also includes `release-metadata.txt` containing application ID, version values, commit, ref, and workflow run ID.

## Pull-request release gate

The existing Android CI validates both unsigned and signed release code paths without using repository secrets.

First it builds:

```text
:app:lintDebug
:app:testDebugUnitTest
:app:assembleDebug
:app:lintRelease
:app:testReleaseUnitTest
:app:assembleRelease
```

With no signing environment present, the release output is unsigned. CI scans both the debug APK and this unsigned release APK for known AI Control HUD credential/local-state artifacts.

CI then creates a short-lived **ephemeral test keystore** with `keytool`, sets the same `HUD_ANDROID_*` Gradle environment contract used by production signing, injects synthetic release version metadata, rebuilds the release APK, verifies it with Android `apksigner`, and confirms the manifest version with `aapt`.

The ephemeral key:

- is generated inside the GitHub-hosted runner;
- is not a repository secret or production credential;
- is never uploaded as an artifact;
- is removed in an `if: always()` cleanup step.

This exercises the actual Gradle release-signing and versioning configuration on every relevant pull request while keeping real signing credentials unavailable to PR jobs.

## Tag/manual production release

For a `v*` tag or manual `.github/workflows/android-release.yml` run:

1. release `versionName/versionCode` are resolved and exported;
2. all four signing secrets must be present;
3. the base64 keystore is decoded only into `$RUNNER_TEMP/ai-control-hud-release.jks`;
4. Gradle receives the temporary keystore path and passwords through environment variables;
5. the project runs release lint, unit tests, and `assembleRelease`;
6. Android `apksigner verify --verbose --print-certs` verifies the produced APK;
7. `aapt dump badging` verifies embedded version metadata;
8. the credential/local-state scanner runs against the signed APK;
9. SHA256 and release metadata files are generated next to the APK;
10. only the APK, SHA256, and non-secret release metadata are uploaded;
11. the materialized keystore is removed in an `if: always()` cleanup step.

The keystore and passwords are never included in the artifact list or repository files.

## Gradle signing configuration

`android/app/build.gradle` configures release signing only when all of these environment variables are present:

```text
HUD_ANDROID_KEYSTORE_PATH
HUD_ANDROID_KEYSTORE_PASSWORD
HUD_ANDROID_KEY_ALIAS
HUD_ANDROID_KEY_PASSWORD
```

A partially configured environment fails Gradle configuration rather than silently producing an unexpectedly unsigned build.

With no signing variables, `assembleRelease` remains available and produces the unsigned PR-gate APK.

## APK secret scan

`scripts/ci-android-apk-secret-scan.py` inspects ZIP entry names and decompressed entry contents. It fails on known project credential/local-state artifacts such as:

- DPAPI credential filenames;
- Hub token filenames/environment names;
- CommandCode key environment names/provider mirror filename;
- ZCode private database/config paths;
- packaged keystores/private-key files;
- packaged SQLite/database files.

Android CI includes a synthetic-leak negative test so the scanner itself cannot silently become a no-op.

This is a targeted release guard, not a general malware/secret scanner. The primary security boundary remains architectural: vendor and Hub-ingest credentials stay on Windows and are never inputs to the Android build.

## Reproducibility definition

The release gate uses pinned JDK/Android SDK/Gradle versions and a stable signing/versioning procedure, and every production release APK gets a SHA256 manifest plus explicit release metadata. The project does **not** currently claim byte-for-byte reproducible Android APK output across independent builds; that would require a separate verified reproducible-build effort.
