# Android release signing

The normal pull-request Android build does not require or receive release-signing secrets. Signed release APKs are produced only by `.github/workflows/android-release.yml` on a `v*` tag or an explicit `workflow_dispatch` run.

## Required GitHub Secrets

Configure these repository/environment secrets before running a signed release:

```text
HUD_ANDROID_KEYSTORE_BASE64
HUD_ANDROID_KEYSTORE_PASSWORD
HUD_ANDROID_KEY_ALIAS
HUD_ANDROID_KEY_PASSWORD
```

`HUD_ANDROID_KEYSTORE_BASE64` is the complete release keystore encoded as base64. The keystore file itself must never be committed.

The repository `.gitignore` excludes common keystore/private-key extensions including `.jks`, `.keystore`, `.p12`, `.pfx`, `.pem`, and `.key`.

## Workflow behavior

### Pull requests

The release workflow builds an **unsigned** release variant with no signing secrets:

```text
:app:lintRelease
:app:testReleaseUnitTest
:app:assembleRelease
```

It then scans the generated APK for known AI Control HUD credential/local-state artifacts. This validates the release build configuration without exposing release credentials to pull requests.

The normal Android CI also scans the debug APK.

### Tag/manual release

For a `v*` tag or manual run:

1. all four signing secrets must be present;
2. the base64 keystore is decoded only into `$RUNNER_TEMP/ai-control-hud-release.jks`;
3. Gradle receives the temporary keystore path and passwords through environment variables;
4. the project builds the release variant;
5. Android `apksigner verify --verbose --print-certs` verifies the produced APK;
6. the credential/local-state scanner runs against the signed APK;
7. a SHA256 file is generated next to the APK;
8. only the APK and SHA256 file are uploaded as the release artifact;
9. the materialized keystore is removed in an `if: always()` cleanup step.

The keystore and passwords are never included in the artifact list or repository files.

## Gradle configuration

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

The release gate uses pinned JDK/Android SDK/Gradle versions and a stable signing procedure, and every produced release APK gets a SHA256 manifest. The project does **not** currently claim byte-for-byte reproducible Android APK output across independent builds; that would require a separate verified reproducible-build effort.
