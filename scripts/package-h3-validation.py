from __future__ import annotations

import argparse
import os
import shutil
import tempfile
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_OUTPUT = ROOT / "dist" / "ai-control-hub-h3-validation.zip"

INCLUDE_FILES = (
    "pyproject.toml",
    "docs/HUB_DEPLOYMENT.md",
    "docs/H3_FIELD_VALIDATION.md",
    "scripts/ai-control-hub-systemd.sh",
    "scripts/backup_hub_db.py",
)
INCLUDE_DIRS = (
    "server",
    "tools",
)


def copy_bundle_tree(destination: Path) -> None:
    for relative in INCLUDE_FILES:
        source = ROOT / relative
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)

    for relative in INCLUDE_DIRS:
        source = ROOT / relative
        target = destination / relative
        shutil.copytree(
            source,
            target,
            ignore=shutil.ignore_patterns(
                "__pycache__",
                "*.pyc",
                ".pytest_cache",
            ),
        )


def write_build_info(destination: Path) -> None:
    sha = os.environ.get("GITHUB_SHA", "local")
    ref = os.environ.get("GITHUB_REF_NAME", "local")
    text = (
        "AI Control HUD H3 validation bundle\n"
        f"commit={sha}\n"
        f"ref={ref}\n"
        "entrypoint=docs/H3_FIELD_VALIDATION.md\n"
    )
    (destination / "BUILD_INFO.txt").write_text(text, encoding="utf-8")


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
    parser = argparse.ArgumentParser(description="Build the CentOS H3 field-validation bundle")
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()

    with tempfile.TemporaryDirectory(prefix="ai-control-h3-") as temp:
        staging = Path(temp) / "ai-control-hub-h3-validation"
        staging.mkdir()
        copy_bundle_tree(staging)
        write_build_info(staging)
        create_zip(staging, args.output.resolve())

    print(args.output.resolve())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
