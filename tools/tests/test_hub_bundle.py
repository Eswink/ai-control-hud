from __future__ import annotations

import hashlib
import sys
import zipfile
from pathlib import Path

import pytest


SCRIPTS = Path(__file__).resolve().parents[2] / "scripts"
if str(SCRIPTS) not in sys.path:
    sys.path.insert(0, str(SCRIPTS))

import hub_bundle  # noqa: E402


def test_production_bundle_records_version_component_and_checksums(tmp_path: Path, monkeypatch) -> None:
    binary = tmp_path / "ai-control-hub"
    binary.write_bytes(b"synthetic-static-binary")
    output = tmp_path / "hub.zip"

    monkeypatch.setenv("GITHUB_SHA", "0123456789abcdef")
    monkeypatch.setenv("GITHUB_REF_NAME", "v1.2.3")

    created = hub_bundle.package_bundle(binary, output, "1.2.3")
    assert created == output.resolve()

    with zipfile.ZipFile(created) as archive:
        names = set(archive.namelist())
        assert "ai-control-hub" in names
        assert "scripts/ai-control-hub-systemd.sh" in names
        assert "scripts/ai-control-hub-lan-doctor.sh" in names
        assert "scripts/ai-control-hub-upgrade.sh" in names
        assert "docs/HUB_DEPLOYMENT.md" in names
        assert "docs/HUB_UPGRADE.md" in names
        assert "docs/HUB_RETENTION.md" in names
        assert "docs/HUB_STORAGE.md" in names
        assert "BUILD_INFO.txt" in names
        assert "SHA256SUMS" in names

        build_info = archive.read("BUILD_INFO.txt").decode("utf-8").splitlines()
        assert build_info[0] == "AI Control HUD Go Hub production bundle"
        assert "bundle_schema=1" in build_info
        assert "component=ai-control-hub" in build_info
        assert "version=1.2.3" in build_info
        assert "commit=0123456789abcdef" in build_info
        assert "ref=v1.2.3" in build_info
        assert "runtime=standalone-go-binary" in build_info
        assert "python_required=false" in build_info
        assert "entrypoint=docs/HUB_DEPLOYMENT.md" in build_info

        checksum_rows = archive.read("SHA256SUMS").decode("utf-8").splitlines()
        protected = set()
        for row in checksum_rows:
            digest, name = row.split("  ", 1)
            protected.add(name)
            assert hashlib.sha256(archive.read(name)).hexdigest() == digest
        assert protected == names - {"SHA256SUMS"}


def test_invalid_bundle_version_is_rejected(tmp_path: Path) -> None:
    binary = tmp_path / "ai-control-hub"
    binary.write_bytes(b"binary")

    with pytest.raises(ValueError, match="invalid Hub bundle version"):
        hub_bundle.package_bundle(binary, tmp_path / "bad.zip", "release/latest")
