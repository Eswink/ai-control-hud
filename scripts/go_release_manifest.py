from __future__ import annotations

import argparse
import hashlib
import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable


MANIFEST_SCHEMA_VERSION = 1
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:[.-][0-9A-Za-z.-]+)?$")
COMMIT_RE = re.compile(r"^[0-9a-fA-F]{7,64}$")
PLATFORM_RE = re.compile(r"^[a-z0-9]+-[a-z0-9]+$")
ALLOWED_COMPONENTS = frozenset({"ai-control-agent", "ai-control-hub"})
REQUIRED_TARGETS = frozenset(
    {
        ("ai-control-agent", "windows-amd64"),
        ("ai-control-agent", "linux-amd64"),
        ("ai-control-agent", "darwin-amd64"),
        ("ai-control-agent", "darwin-arm64"),
        ("ai-control-hub", "linux-amd64"),
    }
)


class ReleaseManifestError(ValueError):
    pass


class UnsupportedReleaseManifestVersion(ReleaseManifestError):
    def __init__(self, version: int) -> None:
        super().__init__(f"unsupported Go release manifest schemaVersion: {version}")
        self.version = version


@dataclass(frozen=True)
class ArtifactSpec:
    component: str
    platform: str
    path: Path


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def parse_artifact_spec(raw: str) -> ArtifactSpec:
    try:
        component, platform, path_text = raw.split(":", 2)
    except ValueError as exc:
        raise ReleaseManifestError(
            "artifact must use COMPONENT:PLATFORM:PATH"
        ) from exc
    component = component.strip()
    platform = platform.strip()
    path_text = path_text.strip()
    if component not in ALLOWED_COMPONENTS:
        raise ReleaseManifestError(f"unsupported release component: {component}")
    if not PLATFORM_RE.fullmatch(platform):
        raise ReleaseManifestError(f"invalid release platform: {platform}")
    if not path_text:
        raise ReleaseManifestError("artifact path is required")
    return ArtifactSpec(component, platform, Path(path_text))


def _validate_version_commit(version: str, commit: str) -> None:
    if not VERSION_RE.fullmatch(version):
        raise ReleaseManifestError(f"invalid release version: {version}")
    if not COMMIT_RE.fullmatch(commit):
        raise ReleaseManifestError(f"invalid release commit: {commit}")


def build_manifest(version: str, commit: str, specs: Iterable[ArtifactSpec]) -> dict[str, Any]:
    _validate_version_commit(version, commit)
    rows: list[dict[str, Any]] = []
    seen_targets: set[tuple[str, str]] = set()
    seen_filenames: set[str] = set()

    for spec in specs:
        target = (spec.component, spec.platform)
        if target in seen_targets:
            raise ReleaseManifestError(
                f"duplicate release target: {spec.component}/{spec.platform}"
            )
        path = spec.path.resolve()
        if not path.is_file():
            raise ReleaseManifestError(f"release artifact not found: {path}")
        filename = path.name
        if filename in seen_filenames:
            raise ReleaseManifestError(f"duplicate release filename: {filename}")
        seen_targets.add(target)
        seen_filenames.add(filename)
        rows.append(
            {
                "component": spec.component,
                "platform": spec.platform,
                "filename": filename,
                "sizeBytes": path.stat().st_size,
                "sha256": sha256_file(path),
            }
        )

    if seen_targets != REQUIRED_TARGETS:
        missing = sorted(REQUIRED_TARGETS - seen_targets)
        extra = sorted(seen_targets - REQUIRED_TARGETS)
        raise ReleaseManifestError(
            f"release target set mismatch: missing={missing} extra={extra}"
        )

    rows.sort(key=lambda row: (row["component"], row["platform"]))
    return {
        "schemaVersion": MANIFEST_SCHEMA_VERSION,
        "releaseVersion": version,
        "commit": commit.lower(),
        "artifacts": rows,
    }


def write_manifest(path: Path, manifest: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(manifest, indent=2, ensure_ascii=False, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def load_manifest(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ReleaseManifestError("Go release manifest could not be read") from exc
    if not isinstance(value, dict):
        raise ReleaseManifestError("Go release manifest root must be an object")
    version = value.get("schemaVersion")
    if isinstance(version, bool) or not isinstance(version, int):
        raise ReleaseManifestError("Go release manifest schemaVersion must be an integer")
    if version != MANIFEST_SCHEMA_VERSION:
        raise UnsupportedReleaseManifestVersion(version)
    return value


def verify_manifest(path: Path, directory: Path) -> dict[str, Any]:
    manifest = load_manifest(path)
    release_version = manifest.get("releaseVersion")
    commit = manifest.get("commit")
    if not isinstance(release_version, str) or not isinstance(commit, str):
        raise ReleaseManifestError("Go release manifest version/commit metadata is invalid")
    _validate_version_commit(release_version, commit)

    raw_artifacts = manifest.get("artifacts")
    if not isinstance(raw_artifacts, list):
        raise ReleaseManifestError("Go release manifest artifacts must be an array")

    specs: list[ArtifactSpec] = []
    expected_rows: dict[tuple[str, str], dict[str, Any]] = {}
    for raw in raw_artifacts:
        if not isinstance(raw, dict):
            raise ReleaseManifestError("Go release manifest artifact must be an object")
        component = raw.get("component")
        platform = raw.get("platform")
        filename = raw.get("filename")
        size_bytes = raw.get("sizeBytes")
        digest = raw.get("sha256")
        if component not in ALLOWED_COMPONENTS:
            raise ReleaseManifestError(f"unsupported release component: {component}")
        if not isinstance(platform, str) or not PLATFORM_RE.fullmatch(platform):
            raise ReleaseManifestError(f"invalid release platform: {platform}")
        if not isinstance(filename, str) or Path(filename).name != filename:
            raise ReleaseManifestError("release artifact filename must be a basename")
        if isinstance(size_bytes, bool) or not isinstance(size_bytes, int) or size_bytes < 0:
            raise ReleaseManifestError("release artifact sizeBytes is invalid")
        if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise ReleaseManifestError("release artifact sha256 is invalid")
        target = (component, platform)
        if target in expected_rows:
            raise ReleaseManifestError(f"duplicate release target: {component}/{platform}")
        expected_rows[target] = raw
        specs.append(ArtifactSpec(component, platform, directory / filename))

    rebuilt = build_manifest(release_version, commit, specs)
    rebuilt_rows = {
        (row["component"], row["platform"]): row for row in rebuilt["artifacts"]
    }
    if rebuilt_rows != expected_rows:
        raise ReleaseManifestError("Go release manifest artifact metadata does not match files")
    return manifest


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build or verify AI Control HUD Go release manifest")
    sub = parser.add_subparsers(dest="command", required=True)

    build = sub.add_parser("build")
    build.add_argument("--version", required=True)
    build.add_argument("--commit", required=True)
    build.add_argument("--output", required=True, type=Path)
    build.add_argument("--artifact", action="append", required=True)

    verify = sub.add_parser("verify")
    verify.add_argument("--manifest", required=True, type=Path)
    verify.add_argument("--directory", required=True, type=Path)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        if args.command == "build":
            specs = [parse_artifact_spec(raw) for raw in args.artifact]
            manifest = build_manifest(args.version, args.commit, specs)
            write_manifest(args.output, manifest)
            print(args.output.resolve())
        else:
            manifest = verify_manifest(args.manifest, args.directory.resolve())
            print(
                "Go release manifest verified: "
                f"version={manifest['releaseVersion']} artifacts={len(manifest['artifacts'])}"
            )
    except ReleaseManifestError as exc:
        print(f"Go release manifest error: {exc}")
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
