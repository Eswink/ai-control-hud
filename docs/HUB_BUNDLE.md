# Go Hub production bundle

The supported Hub release/deployment package is produced by:

```text
python scripts/package-hub-bundle.py \
  --hub-binary PATH/TO/ai-control-hub \
  --version X.Y.Z \
  --output ai-control-hub_X.Y.Z_linux-amd64.zip
```

The historical `scripts/package-h3-validation.py` command remains as a compatibility wrapper around the same implementation. New CI/release automation should use `package-hub-bundle.py`.

## Bundle contents

The ZIP root contains:

```text
ai-control-hub
BUILD_INFO.txt
SHA256SUMS
scripts/ai-control-hub-systemd.sh
scripts/ai-control-hub-lan-doctor.sh
docs/HUB_DEPLOYMENT.md
docs/HUB_LAN_DOCTOR.md
docs/H3_FIELD_VALIDATION.md
docs/H3_WINDOWS_FIRST_INSTALL.md
```

H3 documents remain for field-history and first-install troubleshooting, but the production entrypoint is `docs/HUB_DEPLOYMENT.md`.

## BUILD_INFO contract

`BUILD_INFO.txt` is line-oriented metadata with:

```text
AI Control HUD Go Hub production bundle
bundle_schema=1
component=ai-control-hub
version=<embedded binary version>
commit=<build commit or local>
ref=<build ref or local>
runtime=standalone-go-binary
python_required=false
entrypoint=docs/HUB_DEPLOYMENT.md
```

`bundle_schema` versions the packaging metadata/layout contract independently from the HTTP state/event schemas.

Production CI passes the same version that is embedded into the Go binary and then verifies both values after extracting the archive. Consumers therefore do not need to infer the Hub version from a PR number, filename, or Git ref.

## Integrity

`SHA256SUMS` covers every bundle file except `SHA256SUMS` itself, including `BUILD_INFO.txt` and the Hub binary.

Verify after extraction:

```bash
sha256sum -c SHA256SUMS
./ai-control-hub version
```

The outer release archive also receives its own sidecar checksum in the unified Go release pipeline.

## Compatibility wrapper

The historical command remains valid but now requires an explicit version just like the production entrypoint:

```text
python scripts/package-h3-validation.py \
  --hub-binary PATH/TO/ai-control-hub \
  --version X.Y.Z \
  --output legacy-name.zip
```

It produces the same production bundle contract. The wrapper exists to avoid abruptly breaking scripts that still reference the old filename; it does not preserve the former H3-only metadata format.
