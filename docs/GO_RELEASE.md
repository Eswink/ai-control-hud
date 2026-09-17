# Unified Go release

The versioned Go release contains both production Go components:

```text
Windows/macOS/Linux ai-control-agent
Linux/amd64         ai-control-hub deployment bundle
```

The workflow file remains `.github/workflows/go-release.yml`. Its visible workflow name stays `Go Agent Release` for check-name continuity, but the combined release artifact is an AI Control HUD Go release rather than Agent-only output.

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

Targets and archive contracts:

```text
windows-amd64.zip
  ai-control-agent.exe

linux-amd64.tar.gz
  ai-control-agent
  ai-control-agent-systemd.sh
  ai-control-agent-unix-upgrade.sh

darwin-amd64.tar.gz
  ai-control-agent
  ai-control-agent-launchd.sh
  ai-control-agent-unix-upgrade.sh

darwin-arm64.tar.gz
  ai-control-agent
  ai-control-agent-launchd.sh
  ai-control-agent-unix-upgrade.sh
```

The package builder uses deterministic entry ordering, fixed timestamps/ownership, and executable modes. PR release CI extracts every Agent archive and verifies the platform-specific members. Linux/amd64 is additionally executed after extraction to prove its embedded `version` matches the release version.

The Unix service adapters expose transactional `upgrade --agent PATH` and delegate binary swap/rollback to the shared `ai-control-agent-unix-upgrade.sh`. Runtime machine config and protected SecretStores remain host state and are never placed in release archives.

## Hub target

The Hub release target is Linux/amd64 only. CI builds the standalone `ai-control-hub` binary twice with the same reproducibility flags and requires byte equality and static linking.

It then runs the production packager:

```text
scripts/package-hub-bundle.py
```

producing:

```text
ai-control-hub_<version>_linux-amd64.zip
```

The bundle includes the executable plus supported production assets:

- hardened systemd installer;
- trusted-LAN readiness doctor;
- transactional binary-upgrade helper;
- Hub deployment and upgrade runbooks;
- field-validation/first-install notes;
- `BUILD_INFO.txt`;
- internal `SHA256SUMS` covering every bundle member except the checksum file itself.

The historical `package-h3-validation.py` name remains only as a compatibility wrapper around the same bundle implementation; it is not the production packaging entrypoint.

Release CI extracts the production bundle, executes `ai-control-hub version`, verifies BUILD_INFO, verifies the internal checksums, and requires the systemd/LAN-doctor/upgrade/runbook files to be present.

## Unified manifest

Every target archive has a sidecar `.sha256`. The manifest job downloads all Agent and Hub target artifacts, verifies every sidecar, and generates a top-level:

```text
SHA256SUMS
```

covering all `.zip` and `.tar.gz` archives.

The same job builds and immediately verifies `RELEASE_MANIFEST.json` schema v1. It records the exact required target set, component/platform identity, archive filename, byte size, SHA-256, release version, and build commit. Missing, duplicate, unexpected, or modified archives fail verification.

The combined Actions artifact is:

```text
ai-control-hud-go-release-<version>
```

## Attestation and tag publishing

For manual/tag releases (not pull requests), GitHub artifact attestation covers all Agent/Hub archives plus `SHA256SUMS` and `RELEASE_MANIFEST.json`.

A pushed `v*` tag publishes the combined files as one GitHub Release titled:

```text
AI Control HUD <version>
```

The Android production-signed APK has its own signing workflow because its keystore/security model differs from Go artifact attestation.

## Security boundaries

Go release artifacts contain no Hub bearer token, CommandCode API key, DPAPI record, Unix SecretStore record, ZCode database, local `.env`, or Android signing key. Runtime credentials are configured after installation using the documented platform SecretStore/token-file paths.

Binary upgrade helpers are deliberately credential-blind: they validate and replace executables while preserving existing service definitions and host state rather than re-running credential import or installation.
