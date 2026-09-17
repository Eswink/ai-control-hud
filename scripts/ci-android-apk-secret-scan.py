from __future__ import annotations

import argparse
import sys
import zipfile
from pathlib import Path

BANNED_ENTRY_SUFFIXES = (
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
)

BANNED_CONTENT_MARKERS = (
    b"commandcode-provider.json",
    b"commandcode.dpapi",
    b"hub.dpapi",
    b"ai-control-hub.token",
    b"command_code_api_key",
    b"hud_hub_agent_token",
    b"authorization: bearer",
    b".zcode/cli/db",
    b".zcode\\cli\\db",
    b".zcode/v2/config.json",
    b".zcode\\v2\\config.json",
    b"c:\\programdata\\aicontrolhud",
)

CHUNK_SIZE = 1024 * 1024


def _scan_stream(stream, markers: tuple[bytes, ...]) -> bytes | None:
    max_marker = max(len(marker) for marker in markers)
    carry = b""
    while True:
        chunk = stream.read(CHUNK_SIZE)
        if not chunk:
            return None
        combined = (carry + chunk).lower()
        for marker in markers:
            if marker in combined:
                return marker
        carry = combined[-(max_marker - 1) :] if max_marker > 1 else b""


def scan_apk(path: Path) -> list[str]:
    findings: list[str] = []
    if not path.is_file():
        return [f"APK not found: {path}"]

    try:
        with zipfile.ZipFile(path) as archive:
            for info in archive.infolist():
                name = info.filename.lower()
                if name.endswith(BANNED_ENTRY_SUFFIXES):
                    findings.append(f"forbidden packaged file: {info.filename}")
                    continue
                if info.is_dir():
                    continue
                with archive.open(info, "r") as stream:
                    marker = _scan_stream(stream, BANNED_CONTENT_MARKERS)
                if marker is not None:
                    findings.append(
                        f"forbidden credential/local-state marker {marker.decode('ascii', 'replace')!r} "
                        f"in {info.filename}"
                    )
    except (OSError, zipfile.BadZipFile) as exc:
        findings.append(f"cannot inspect APK {path}: {type(exc).__name__}")
    return findings


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fail if an Android APK contains known AI Control HUD credential/local-state artifacts."
    )
    parser.add_argument("apk", nargs="+", type=Path)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    failed = False
    for apk in args.apk:
        findings = scan_apk(apk)
        if findings:
            failed = True
            print(f"[apk-secret-scan] FAIL {apk}", file=sys.stderr)
            for finding in findings:
                print(f"  - {finding}", file=sys.stderr)
        else:
            print(f"[apk-secret-scan] PASS {apk}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
