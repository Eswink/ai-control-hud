from __future__ import annotations

import argparse
import sys
from pathlib import Path

BANNED_MARKERS = (
    "HUD_HUB_AGENT_TOKEN",
    "command_code_api_key",
    "authorization: bearer",
    "commandcode.dpapi",
    "hub.dpapi",
    "ai-control-hub.token",
    ".zcode/cli/db",
    ".zcode\\cli\\db",
    ".zcode/v2/config.json",
    ".zcode\\v2\\config.json",
    "c:\\programdata\\aicontrolhud",
)
CHUNK_SIZE = 1024 * 1024


def encoded_markers() -> tuple[tuple[str, bytes], ...]:
    values: list[tuple[str, bytes]] = []
    for marker in BANNED_MARKERS:
        values.append((marker, marker.lower().encode("utf-8")))
        values.append((marker + " [UTF-16LE]", marker.lower().encode("utf-16le")))
    return tuple(values)


def scan_file(path: Path) -> list[str]:
    if not path.is_file():
        return [f"artifact not found: {path}"]

    markers = encoded_markers()
    max_marker = max(len(value) for _, value in markers)
    carry = b""
    try:
        with path.open("rb") as stream:
            while True:
                chunk = stream.read(CHUNK_SIZE)
                if not chunk:
                    break
                combined = carry + chunk.lower()
                for label, marker in markers:
                    if marker in combined:
                        return [f"forbidden credential/local-state marker {label!r}"]
                carry = combined[-(max_marker - 1) :] if max_marker > 1 else b""
    except OSError as exc:
        return [f"cannot inspect artifact: {type(exc).__name__}"]
    return []


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fail if a native Windows UI artifact contains known credential/local-state markers."
    )
    parser.add_argument("artifact", nargs="+", type=Path)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    failed = False
    for artifact in args.artifact:
        findings = scan_file(artifact)
        if findings:
            failed = True
            print(f"[windows-ui-artifact-scan] FAIL {artifact}", file=sys.stderr)
            for finding in findings:
                print(f"  - {finding}", file=sys.stderr)
        else:
            print(f"[windows-ui-artifact-scan] PASS {artifact}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
