#!/usr/bin/env python3
"""Build a deterministic native Windows Agent UI delivery ZIP."""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import stat
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:[.-][0-9A-Za-z.-]+)?$")
FIXED_TIME = (1980, 1, 1, 0, 0, 0)


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def zip_info(name: str, mode: int) -> zipfile.ZipInfo:
    info = zipfile.ZipInfo(name, FIXED_TIME)
    info.compress_type = zipfile.ZIP_DEFLATED
    info.create_system = 3
    info.external_attr = (mode & 0xFFFF) << 16
    return info


def add_bytes(archive: zipfile.ZipFile, name: str, data: bytes, mode: int) -> None:
    archive.writestr(zip_info(name, mode), data, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)


def package(exe: Path, metrics: Path, version: str, output: Path) -> Path:
    exe = exe.resolve()
    metrics = metrics.resolve()
    output = output.resolve()
    if not exe.is_file():
        raise FileNotFoundError(f"Windows UI executable not found: {exe}")
    if not metrics.is_file():
        raise FileNotFoundError(f"Windows UI resource metrics not found: {metrics}")
    if not VERSION_RE.fullmatch(version):
        raise ValueError(f"invalid Windows UI version: {version}")

    readme = ROOT / "windows-ui" / "README.md"
    if not readme.is_file():
        raise FileNotFoundError(f"Windows UI README not found: {readme}")

    build_info = (
        "AI Control HUD native Windows Agent UI\n"
        "bundle_schema=1\n"
        "component=ai-control-agent-ui\n"
        f"version={version}\n"
        f"commit={os.environ.get('GITHUB_SHA', 'local')}\n"
        "architecture=windows-amd64-native\n"
        "runtime=win32-direct2d-directwrite-winhttp\n"
        "agent_service_separate=true\n"
    ).encode("utf-8")

    members: list[tuple[str, bytes, int]] = [
        ("ai-control-agent-ui.exe", exe.read_bytes(), stat.S_IFREG | 0o755),
        ("resource-metrics.json", metrics.read_bytes(), stat.S_IFREG | 0o644),
        ("README.md", readme.read_bytes(), stat.S_IFREG | 0o644),
        ("BUILD_INFO.txt", build_info, stat.S_IFREG | 0o644),
    ]
    checksums = "".join(
        f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data, _ in sorted(members)
    ).encode("utf-8")
    members.append(("SHA256SUMS", checksums, stat.S_IFREG | 0o644))

    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(prefix=output.name, suffix=".tmp", dir=output.parent, delete=False) as temporary:
        temporary_path = Path(temporary.name)
    try:
        with zipfile.ZipFile(temporary_path, "w") as archive:
            for name, data, mode in sorted(members):
                add_bytes(archive, name, data, mode)
        temporary_path.replace(output)
    finally:
        temporary_path.unlink(missing_ok=True)
    return output


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--exe", required=True, type=Path)
    parser.add_argument("--metrics", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        created = package(args.exe, args.metrics, args.version, args.output)
    except (FileNotFoundError, ValueError) as exc:
        parser.error(str(exc))
    print(created)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
