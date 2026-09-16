from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import tempfile
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUNDLE_SCHEMA = 1
DEFAULT_OUTPUT = ROOT / "dist" / "ai-control-hub-linux-amd64.zip"
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:[.-][0-9A-Za-z.-]+)?$")

INCLUDE_FILES = (
    "docs/HUB_DEPLOYMENT.md",
    "docs/HUB_LAN_DOCTOR.md",
    "docs/H3_FIELD_VALIDATION.md",
    "docs/H3_WINDOWS_FIRST_INSTALL.md",
    "scripts/ai-control-hub-systemd.sh",
    "scripts/ai-control-hub-lan-doctor.sh",
)


def copy_bundle_tree(destination: Path, hub_binary: Path) -> None:
    for relative in INCLUDE_FILES:
        source = ROOT / relative
        if not source.is_file():
            raise FileNotFoundError(f"required Hub bundle file is missing: {source}")
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)

    binary_target = destination / "ai-control-hub"
    shutil.copy2(hub_binary, binary_target)
    binary_target.chmod(0o755)


def build_info_text(version: str) -> str:
    sha = os.environ.get("GITHUB_SHA", "local")
    ref = os.environ.get("GITHUB_REF_NAME", "local")
    return (
        "AI Control HUD Go Hub production bundle\n"
        f"bundle_schema={BUNDLE_SCHEMA}\n"
        "component=ai-control-hub\n"
        f"version={version}\n"
        f"commit={sha}\n"
        f"ref={ref}\n"
        "runtime=standalone-go-binary\n"
        "python_required=false\n"
        "entrypoint=docs/HUB_DEPLOYMENT.md\n"
    )


def write_build_info(destination: Path, version: str) -> None:
    (destination / "BUILD_INFO.txt").write_text(build_info_text(version), encoding="utf-8")


def write_checksums(destination: Path) -> None:
    rows: list[str] = []
    for path in sorted(destination.rglob("*")):
        if not path.is_file() or path.name == "SHA256SUMS":
            continue
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        rows.append(f"{digest}  {path.relative_to(destination).as_posix()}")
    (destination / "SHA256SUMS").write_text("\n".join(rows) + "\n", encoding="utf-8")


def create_zip(source_dir: Path, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    tmp = output.with_suffix(output.suffix + ".tmp")
    tmp.unlink(missing_ok=True)

    with zipfile.ZipFile(tmp, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in sorted(source_dir.rglob("*")):
            if not path.is_file():
                continue
            archive.write(path, path.relative_to(source_dir).as_posix())

    tmp.replace(output)


def package_bundle(hub_binary: Path, output: Path, version: str) -> Path:
    hub_binary = hub_binary.resolve()
    output = output.resolve()
    if not hub_binary.is_file():
        raise FileNotFoundError(f"Hub binary not found: {hub_binary}")
    if not VERSION_RE.fullmatch(version):
        raise ValueError(f"invalid Hub bundle version: {version}")

    with tempfile.TemporaryDirectory(prefix="ai-control-hub-bundle-") as temp:
        staging = Path(temp) / "ai-control-hub"
        staging.mkdir()
        copy_bundle_tree(staging, hub_binary)
        write_build_info(staging, version)
        write_checksums(staging)
        create_zip(staging, output)
    return output


def build_parser(*, legacy_h3: bool = False) -> argparse.ArgumentParser:
    description = (
        "Build the standalone Go Hub production bundle"
        if not legacy_h3
        else "Compatibility wrapper for the historical H3 Hub validation bundle"
    )
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument("--hub-binary", type=Path, required=True)
    parser.add_argument("--version", required=True, help="embedded ai-control-hub release version")
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    return parser


def main(argv: list[str] | None = None, *, legacy_h3: bool = False) -> int:
    parser = build_parser(legacy_h3=legacy_h3)
    args = parser.parse_args(argv)
    try:
        output = package_bundle(args.hub_binary, args.output, args.version)
    except (FileNotFoundError, ValueError) as exc:
        parser.error(str(exc))
    print(output)
    return 0
