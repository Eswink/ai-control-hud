from __future__ import annotations

import argparse
import hashlib
import os
import shutil
import tempfile
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_OUTPUT = ROOT / "dist" / "ai-control-hub-h3-validation.zip"

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
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)

    binary_target = destination / "ai-control-hub"
    shutil.copy2(hub_binary, binary_target)
    binary_target.chmod(0o755)


def write_build_info(destination: Path) -> None:
    sha = os.environ.get("GITHUB_SHA", "local")
    ref = os.environ.get("GITHUB_REF_NAME", "local")
    text = (
        "AI Control HUD H3 validation bundle (Go Hub)\n"
        f"commit={sha}\n"
        f"ref={ref}\n"
        "runtime=standalone-go-binary\n"
        "python_required=false\n"
        "entrypoint=docs/H3_FIELD_VALIDATION.md\n"
    )
    (destination / "BUILD_INFO.txt").write_text(text, encoding="utf-8")


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


def main() -> int:
    parser = argparse.ArgumentParser(description="Build the binary-only CentOS H3 field-validation bundle")
    parser.add_argument("--hub-binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()

    hub_binary = args.hub_binary.resolve()
    if not hub_binary.is_file():
        parser.error(f"Hub binary not found: {hub_binary}")

    with tempfile.TemporaryDirectory(prefix="ai-control-h3-") as temp:
        staging = Path(temp) / "ai-control-hub-h3-validation"
        staging.mkdir()
        copy_bundle_tree(staging, hub_binary)
        write_build_info(staging)
        write_checksums(staging)
        create_zip(staging, args.output.resolve())

    print(args.output.resolve())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
