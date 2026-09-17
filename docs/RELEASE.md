# AI Control HUD release contract

This repository publishes one version tag for the Agent, Hub, Windows native UI and Android HUD.

## Versioning

Release tags use:

```text
vMAJOR.MINOR.PATCH
vMAJOR.MINOR.PATCH-suffix
```

The tag version is embedded in Go release binaries and Windows UI bundle metadata and is used as the Android `versionName`.

## Tagged release workflows

A pushed `v*` tag starts two complementary workflows:

1. `Go Agent Release` builds and attests the cross-platform Go Agent and Linux Hub bundle, generates checksums/manifests, and creates the GitHub Release.
2. `UI Release` builds and attests the native Windows UI and signed Android APK, waits for the GitHub Release created by the Go workflow, then attaches the UI assets to the same release.

The final GitHub Release therefore contains the complete supported delivery set rather than only one runtime component.

## Release assets

Expected Go assets include:

- Windows/amd64 Agent archive;
- Linux/amd64 Agent archive;
- macOS amd64 and arm64 Agent archives;
- Linux/amd64 Hub deployment bundle;
- per-target checksum sidecars;
- unified `SHA256SUMS`;
- `RELEASE_MANIFEST.json`.

Expected UI assets include:

- `ai-control-agent-ui_<version>_windows-amd64.zip`;
- its SHA-256 sidecar;
- `ai-control-hud_<version>_android.apk`;
- its SHA-256 sidecar;
- `android-release-metadata.txt`.

The Windows archive contains the native executable, resource metrics, build metadata, documentation and internal checksums.

## Android signing policy

Stable tags (`vMAJOR.MINOR.PATCH`) require all production Android signing secrets:

```text
HUD_ANDROID_KEYSTORE_BASE64
HUD_ANDROID_KEYSTORE_PASSWORD
HUD_ANDROID_KEY_ALIAS
HUD_ANDROID_KEY_PASSWORD
```

A stable release fails closed if these secrets are absent.

Prerelease versions containing a suffix may use an ephemeral CI signing identity when production signing is unavailable. The resulting `android-release-metadata.txt` explicitly records `signing=ephemeral-ci`. Such an APK is suitable for release-candidate validation, not long-lived production update continuity.

Pull-request validation always uses an ephemeral signing identity and never receives production signing secrets.

## Release tag request

The repository includes a guarded `Release Tag Request` workflow because release tags must point to the current `main` commit.

A release request branch is named:

```text
release/v<version>
```

and contains `.release-request`:

```text
version=<version>
commit=<40-character-main-commit-sha>
```

The workflow validates the version, branch name and target SHA, requires the target to equal current `main`, and then creates one annotated immutable `v<version>` tag. Existing tags are never moved.

## Release acceptance

A version is considered published only after:

- the release tag points at the intended `main` commit;
- Go release build/manifest/attestation/publish jobs succeed;
- Windows UI reproducibility, native architecture, secret scan and resource/leak gates succeed;
- Android lint/tests/signature/version/resource/secret gates succeed;
- UI artifact attestation succeeds;
- the GitHub Release contains both Go and UI assets.

The roadmap completion record is `docs/UI_PLAN_COMPLETION.md`.
