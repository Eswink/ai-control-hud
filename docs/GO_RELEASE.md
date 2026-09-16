# Unified Go release

The versioned Go release contains both production Go components:

```text
Windows/macOS/Linux ai-control-agent
Linux/amd64         ai-control-hub deployment bundle
```

The existing workflow file remains `.github/workflows/go-release.yml`. Its visible workflow name stays `Go Agent Release` for check-name continuity, but the combined release artifact is now an AI Control HUD Go release rather than Agent-only output.

## Version sources

The same semantic version is embedded in every Go binary:

- pull request: `0.0.0-pr.<number>`;
- manual workflow: required `version` input;
- `v*` tag: tag name without the leading `v`.

Accepted form:

```text
MAJOR.MINOR.PATCH
MAJOR.MINOR.PATCH-suffix
MAJOR.MINOR.PATCH.suffix
```

Invalid versions fail before packaging.

## Agent targets

The workflow builds `ai-control-agent` twice per target with:

```text
CGO_ENABLED=0
-trimpath
-buildvcs=false
-buildid=
```

and requires byte-for-byte equality before packaging.

Targets:

```text
windows-amd64  zip
linux-amd64    tar.gz
darwin-amd64   tar.gz
darwin-arm64   tar.gz
```

Linux/amd64 is executed after extraction to prove its embedded `version` matches the release version.

## Hub target

The Hub release target is Linux/amd64 only. CI builds the standalone `ai-control-hub` binary twice with the same reproducibility flags and requires byte equality and static linking.

It then runs `scripts/package-h3-validation.py`, producing:

```text
ai-control-hub_<version>_linux-amd64.zip
```

The bundle includes the executable plus the supported production assets:

- hardened systemd installer;
- trusted-LAN readiness doctor;
- Hub deployment runbook;
- field-validation/first-install notes;
- `BUILD_INFO.txt`;
- internal `SHA256SUMS`.

Release CI extracts the bundle, executes `ai-control-hub version`, verifies the internal checksums, and requires the systemd/LAN-doctor/runbook files to be present.

## Unified manifest

Every target archive has a sidecar `.sha256`. The manifest job downloads all Agent and Hub target artifacts, verifies every sidecar, and generates a top-level:

```text
SHA256SUMS
```

covering all `.zip` and `.tar.gz` archives.

The manifest explicitly requires both:

```text
ai-control-agent_<version>_windows-amd64.zip
ai-control-hub_<version>_linux-amd64.zip
```

so a release cannot silently publish only one component.

The combined Actions artifact is:

```text
ai-control-hud-go-release-<version>
```

## Attestation and tag publishing

For manual/tag releases (not pull requests), GitHub artifact attestation covers all Agent/Hub archives and the unified `SHA256SUMS`.

A pushed `v*` tag publishes the combined files as one GitHub Release titled:

```text
AI Control HUD <version>
```

The Android production-signed APK has its own signing workflow because its keystore/security model differs from Go artifact attestation.

## Security boundaries

Go release artifacts contain no Hub bearer token, CommandCode API key, DPAPI record, ZCode database, local `.env`, or Android signing key. Runtime credentials are configured after installation using the documented platform SecretStore/token-file paths.
