from __future__ import annotations

import hashlib
import importlib.util
import sys
import zipfile
from pathlib import Path

import pytest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "package-windows-ui.py"
SPEC = importlib.util.spec_from_file_location("package_windows_ui", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
module = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = module
SPEC.loader.exec_module(module)


def test_windows_ui_bundle_is_deterministic_and_self_verifying(tmp_path: Path, monkeypatch) -> None:
    exe = tmp_path / "ai-control-agent-ui.exe"
    metrics = tmp_path / "resource-metrics.json"
    exe.write_bytes(b"synthetic-native-ui")
    metrics.write_text('{"schemaVersion":2,"workingSetMiB":12.5}\n', encoding="utf-8")

    # The packager intentionally consumes the repository README. Keep this test
    # independent from a fake working tree while exercising the real member.
    monkeypatch.setenv("GITHUB_SHA", "0123456789abcdef")
    first = tmp_path / "first.zip"
    second = tmp_path / "second.zip"
    module.package(exe, metrics, "1.2.3-ui", first)
    module.package(exe, metrics, "1.2.3-ui", second)

    assert first.read_bytes() == second.read_bytes()
    with zipfile.ZipFile(first) as archive:
        names = set(archive.namelist())
        assert names == {
            "BUILD_INFO.txt",
            "README.md",
            "SHA256SUMS",
            "ai-control-agent-ui.exe",
            "resource-metrics.json",
        }
        for info in archive.infolist():
            assert info.date_time == (1980, 1, 1, 0, 0, 0)

        build_info = archive.read("BUILD_INFO.txt").decode("utf-8").splitlines()
        assert "bundle_schema=1" in build_info
        assert "component=ai-control-agent-ui" in build_info
        assert "version=1.2.3-ui" in build_info
        assert "commit=0123456789abcdef" in build_info
        assert "runtime=win32-direct2d-directwrite-winhttp" in build_info

        protected = set()
        for row in archive.read("SHA256SUMS").decode("utf-8").splitlines():
            digest, name = row.split("  ", 1)
            protected.add(name)
            assert hashlib.sha256(archive.read(name)).hexdigest() == digest
        assert protected == names - {"SHA256SUMS"}


def test_windows_ui_bundle_rejects_invalid_version(tmp_path: Path) -> None:
    exe = tmp_path / "ui.exe"
    metrics = tmp_path / "metrics.json"
    exe.write_bytes(b"x")
    metrics.write_text("{}", encoding="utf-8")
    with pytest.raises(ValueError, match="invalid Windows UI version"):
        module.package(exe, metrics, "latest/stable", tmp_path / "bad.zip")
