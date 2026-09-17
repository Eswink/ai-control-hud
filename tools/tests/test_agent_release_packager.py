from __future__ import annotations

import gzip
import importlib.util
import io
import tarfile
import zipfile
from pathlib import Path

import pytest


SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "package-agent-release.py"
SPEC = importlib.util.spec_from_file_location("package_agent_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
packager = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(packager)


def test_targz_entries_are_reproducible_and_include_extras(tmp_path: Path) -> None:
    binary = tmp_path / "ai-control-agent"
    adapter = tmp_path / "adapter.sh"
    helper = tmp_path / "upgrade.sh"
    binary.write_bytes(b"agent-binary")
    adapter.write_text("#!/bin/sh\necho adapter\n", encoding="utf-8")
    helper.write_text("#!/bin/sh\necho helper\n", encoding="utf-8")

    entries = packager.collect_entries(
        binary,
        "ai-control-agent",
        [f"{adapter}=ai-control-agent-systemd.sh", f"{helper}=ai-control-agent-unix-upgrade.sh"],
    )
    first = tmp_path / "first.tar.gz"
    second = tmp_path / "second.tar.gz"
    packager.targz_entries(entries, first)
    packager.targz_entries(entries, second)
    assert first.read_bytes() == second.read_bytes()

    with gzip.open(first, "rb") as compressed:
        with tarfile.open(fileobj=io.BytesIO(compressed.read()), mode="r:") as archive:
            names = archive.getnames()
            assert names == [
                "ai-control-agent",
                "ai-control-agent-systemd.sh",
                "ai-control-agent-unix-upgrade.sh",
            ]
            for member in archive.getmembers():
                assert member.mode == 0o755
                assert member.uid == 0
                assert member.gid == 0
                assert member.mtime == 0


def test_zip_entries_remain_deterministic(tmp_path: Path) -> None:
    binary = tmp_path / "ai-control-agent.exe"
    binary.write_bytes(b"windows-agent")
    entries = packager.collect_entries(binary, "ai-control-agent.exe", [])
    first = tmp_path / "first.zip"
    second = tmp_path / "second.zip"
    packager.zip_entries(entries, first)
    packager.zip_entries(entries, second)
    assert first.read_bytes() == second.read_bytes()
    with zipfile.ZipFile(first) as archive:
        assert archive.namelist() == ["ai-control-agent.exe"]
        info = archive.getinfo("ai-control-agent.exe")
        assert (info.external_attr >> 16) & 0o777 == 0o755


def test_invalid_extra_archive_path_is_rejected(tmp_path: Path) -> None:
    binary = tmp_path / "agent"
    extra = tmp_path / "helper"
    binary.write_bytes(b"agent")
    extra.write_bytes(b"helper")
    with pytest.raises(ValueError, match="invalid archive path"):
        packager.collect_entries(binary, "ai-control-agent", [f"{extra}=../helper"])


def test_duplicate_archive_entry_is_rejected(tmp_path: Path) -> None:
    binary = tmp_path / "agent"
    extra = tmp_path / "helper"
    binary.write_bytes(b"agent")
    extra.write_bytes(b"helper")
    with pytest.raises(ValueError, match="unique"):
        packager.collect_entries(binary, "ai-control-agent", [f"{extra}=ai-control-agent"])
