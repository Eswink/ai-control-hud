from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path
from typing import Iterable

BANNED_BASENAMES = {
    "auth.json",
    "local.properties",
    "secrets.properties",
    "ai-control-hub.token",
    "commandcode-provider.json",
    "commandcode.dpapi",
    "hub.dpapi",
}

BANNED_SUFFIXES = (
    ".jks",
    ".keystore",
    ".p12",
    ".pfx",
    ".pem",
    ".key",
    ".dpapi",
    ".sqlite",
    ".sqlite3",
    ".db",
    ".db-wal",
    ".db-shm",
    ".log",
    ".jsonl",
    ".apk",
    ".aab",
)

BANNED_SEGMENTS = {".local"}


def private_key_markers() -> tuple[bytes, ...]:
    prefix = "-----BEGIN "
    suffix = "PRIVATE KEY-----"
    return tuple(
        (prefix + ((kind + " ") if kind else "") + suffix).encode("ascii")
        for kind in ("", "RSA", "EC", "OPENSSH")
    )


def tracked_paths(repo: Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z"],
        cwd=repo,
        check=True,
        stdout=subprocess.PIPE,
    )
    return [item.decode("utf-8", "surrogateescape") for item in result.stdout.split(b"\0") if item]


def path_findings(paths: Iterable[str]) -> list[str]:
    findings: list[str] = []
    for raw in paths:
        normalized = raw.replace("\\", "/")
        parts = tuple(part for part in normalized.split("/") if part)
        name = parts[-1].lower() if parts else ""
        suffix_name = normalized.lower()

        if any(part.lower() in BANNED_SEGMENTS for part in parts):
            findings.append(f"tracked private directory: {raw}")
            continue
        if name == ".env" or (name.startswith(".env.") and name != ".env.example"):
            findings.append(f"tracked environment file: {raw}")
            continue
        if name in BANNED_BASENAMES:
            findings.append(f"tracked private artifact: {raw}")
            continue
        if suffix_name.endswith(BANNED_SUFFIXES):
            findings.append(f"tracked generated/private artifact: {raw}")
    return findings


def private_key_findings(repo: Path, paths: Iterable[str]) -> list[str]:
    findings: list[str] = []
    markers = private_key_markers()
    for raw in paths:
        path = repo / raw
        if not path.is_file():
            continue
        try:
            data = path.read_bytes()
        except OSError:
            continue
        for marker in markers:
            if marker in data:
                findings.append(f"tracked private-key material: {raw}")
                break
    return findings


def scan_repository(repo: Path) -> list[str]:
    paths = tracked_paths(repo)
    return path_findings(paths) + private_key_findings(repo, paths)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fail when Git-tracked AI Control HUD files include local credentials/state/build artifacts."
    )
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    repo = args.repo.resolve()
    try:
        findings = scan_repository(repo)
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"[repository-safety] cannot inspect Git index: {type(exc).__name__}", file=sys.stderr)
        return 2

    if findings:
        print("[repository-safety] FAIL", file=sys.stderr)
        for finding in findings:
            print(f"  - {finding}", file=sys.stderr)
        return 1

    print("[repository-safety] PASS: no tracked private/local/build artifacts detected")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
