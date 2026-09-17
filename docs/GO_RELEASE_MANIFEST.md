# Go release manifest schema v1

The unified Go release includes both the traditional `SHA256SUMS` file and a machine-readable:

```text
RELEASE_MANIFEST.json
```

The JSON manifest is intended for automation such as future update/download tooling. Consumers should not infer component/platform identity by parsing archive filenames when this manifest is available.

## Schema

```json
{
  "schemaVersion": 1,
  "releaseVersion": "1.2.3",
  "commit": "0123456789abcdef...",
  "artifacts": [
    {
      "component": "ai-control-agent",
      "platform": "windows-amd64",
      "filename": "ai-control-agent_1.2.3_windows-amd64.zip",
      "sizeBytes": 123456,
      "sha256": "..."
    },
    {
      "component": "ai-control-hub",
      "platform": "linux-amd64",
      "filename": "ai-control-hub_1.2.3_linux-amd64.zip",
      "sizeBytes": 123456,
      "sha256": "..."
    }
  ]
}
```

There is intentionally no generated timestamp, so identical release inputs can produce identical manifest JSON.

## Required production targets

Schema v1 for this repository requires exactly:

```text
ai-control-agent / windows-amd64
ai-control-agent / linux-amd64
ai-control-agent / darwin-amd64
ai-control-agent / darwin-arm64
ai-control-hub   / linux-amd64
```

A missing or duplicate target is a release error. This prevents an apparently successful release from silently omitting the Hub or one supported Agent target.

## Builder

The release workflow supplies component/platform identity explicitly:

```text
python scripts/go_release_manifest.py build \
  --version 1.2.3 \
  --commit <git-sha> \
  --output dist/RELEASE_MANIFEST.json \
  --artifact ai-control-agent:windows-amd64:dist/...zip \
  ...
```

The builder computes file size and SHA256 itself; these values are not accepted as caller-provided metadata.

## Verification

A downloaded combined release can be checked with:

```text
python scripts/go_release_manifest.py verify \
  --manifest RELEASE_MANIFEST.json \
  --directory .
```

Verification rejects:

- unsupported `schemaVersion`;
- invalid release version/commit metadata;
- missing, duplicate, or unexpected production targets;
- path-bearing filenames instead of basenames;
- malformed size/hash fields;
- missing files;
- any size/SHA256 mismatch.

The release workflow runs the verifier immediately after building the manifest. Unit tests also prove a modified archive is detected.

## Relationship to SHA256SUMS and attestation

`SHA256SUMS` remains a standard human/CLI-friendly checksum list. `RELEASE_MANIFEST.json` adds component/platform metadata for machines.

For tag/manual releases, GitHub artifact attestation covers:

- all Go Agent/Hub archives;
- `SHA256SUMS`;
- `RELEASE_MANIFEST.json`.

A future manifest schema must use a new `schemaVersion`; consumers must reject unsupported future versions rather than guessing new semantics.
