from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest


SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"
if str(SCRIPTS) not in sys.path:
    sys.path.insert(0, str(SCRIPTS))

import go_release_manifest as manifest  # noqa: E402


TARGETS = (
    ("ai-control-agent", "windows-amd64", "ai-control-agent_1.2.3_windows-amd64.zip"),
    ("ai-control-agent", "linux-amd64", "ai-control-agent_1.2.3_linux-amd64.tar.gz"),
    ("ai-control-agent", "darwin-amd64", "ai-control-agent_1.2.3_darwin-amd64.tar.gz"),
    ("ai-control-agent", "darwin-arm64", "ai-control-agent_1.2.3_darwin-arm64.tar.gz"),
    ("ai-control-hub", "linux-amd64", "ai-control-hub_1.2.3_linux-amd64.zip"),
)


def create_artifacts(tmp_path: Path) -> list[manifest.ArtifactSpec]:
    specs = []
    for index, (component, platform, filename) in enumerate(TARGETS):
        path = tmp_path / filename
        path.write_bytes((f"artifact-{index}-" + component + platform).encode("utf-8"))
        specs.append(manifest.ArtifactSpec(component, platform, path))
    return specs


def test_build_and_verify_release_manifest(tmp_path: Path) -> None:
    specs = create_artifacts(tmp_path)
    value = manifest.build_manifest("1.2.3", "0123456789abcdef", specs)

    assert value["schemaVersion"] == 1
    assert value["releaseVersion"] == "1.2.3"
    assert value["commit"] == "0123456789abcdef"
    assert len(value["artifacts"]) == 5
    assert value["artifacts"] == sorted(
        value["artifacts"], key=lambda row: (row["component"], row["platform"])
    )
    for row in value["artifacts"]:
        assert len(row["sha256"]) == 64
        assert row["sizeBytes"] > 0
        assert "/" not in row["filename"]

    path = tmp_path / "RELEASE_MANIFEST.json"
    manifest.write_manifest(path, value)
    verified = manifest.verify_manifest(path, tmp_path)
    assert verified == value

    encoded = path.read_text(encoding="utf-8")
    assert encoded.endswith("\n")
    assert json.loads(encoded) == value


def test_manifest_detects_modified_artifact(tmp_path: Path) -> None:
    specs = create_artifacts(tmp_path)
    value = manifest.build_manifest("1.2.3", "0123456789abcdef", specs)
    path = tmp_path / "RELEASE_MANIFEST.json"
    manifest.write_manifest(path, value)

    specs[0].path.write_bytes(b"tampered")
    with pytest.raises(manifest.ReleaseManifestError, match="does not match"):
        manifest.verify_manifest(path, tmp_path)


def test_missing_target_is_rejected(tmp_path: Path) -> None:
    specs = create_artifacts(tmp_path)[:-1]
    with pytest.raises(manifest.ReleaseManifestError, match="target set mismatch"):
        manifest.build_manifest("1.2.3", "0123456789abcdef", specs)


def test_duplicate_target_is_rejected(tmp_path: Path) -> None:
    specs = create_artifacts(tmp_path)
    duplicate = tmp_path / "duplicate.zip"
    duplicate.write_bytes(b"duplicate")
    specs.append(manifest.ArtifactSpec("ai-control-hub", "linux-amd64", duplicate))
    with pytest.raises(manifest.ReleaseManifestError, match="duplicate release target"):
        manifest.build_manifest("1.2.3", "0123456789abcdef", specs)


def test_future_manifest_schema_is_rejected(tmp_path: Path) -> None:
    path = tmp_path / "future.json"
    path.write_text('{"schemaVersion":2}\n', encoding="utf-8")
    with pytest.raises(manifest.UnsupportedReleaseManifestVersion):
        manifest.load_manifest(path)
