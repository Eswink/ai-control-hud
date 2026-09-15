from __future__ import annotations

import argparse
import json
import os
import platform
import shutil
import sqlite3
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable
from urllib.parse import quote

REPORT_VERSION = 1
MAX_DATABASES = 100
MAX_SCAN_DEPTH = 6


def display_path(path: Path) -> str:
    try:
        path = path.expanduser().resolve()
    except OSError:
        path = path.expanduser()
    home = Path.home().resolve()
    try:
        relative = path.relative_to(home)
        return "~/" + relative.as_posix()
    except ValueError:
        return f"<external>/{path.name}"


def safe_cli_version(names: Iterable[str]) -> dict[str, Any] | None:
    for name in names:
        executable = shutil.which(name)
        if not executable:
            continue
        result: dict[str, Any] = {"name": name, "found": True, "path": display_path(Path(executable))}
        try:
            proc = subprocess.run([executable, "--version"], check=False, capture_output=True, text=True, timeout=3, env={**os.environ, "NO_COLOR": "1"})
            combined = (proc.stdout or proc.stderr).strip().splitlines()
            result["versionOutput"] = combined[0][:200] if combined else None
            result["versionExitCode"] = proc.returncode
        except (OSError, subprocess.SubprocessError) as exc:
            result["versionError"] = type(exc).__name__
        return result
    return None


def _walk_limited(root: Path, suffixes: set[str]) -> list[Path]:
    if not root.is_dir():
        return []
    found: list[Path] = []
    base_depth = len(root.parts)
    for current, dirs, files in os.walk(root):
        current_path = Path(current)
        depth = len(current_path.parts) - base_depth
        if depth >= MAX_SCAN_DEPTH:
            dirs[:] = []
        dirs[:] = [name for name in dirs if name not in {"node_modules", ".git", "cache", "Cache"}]
        for filename in files:
            path = current_path / filename
            if path.suffix.lower() in suffixes:
                found.append(path)
                if len(found) >= MAX_DATABASES:
                    return found
    return found


def sqlite_schema(path: Path) -> dict[str, Any]:
    report: dict[str, Any] = {"path": display_path(path), "readOnly": True}
    try:
        uri = f"file:{quote(str(path.expanduser().resolve()))}?mode=ro"
        conn = sqlite3.connect(uri, uri=True, timeout=1)
        conn.row_factory = sqlite3.Row
        try:
            report["userVersion"] = conn.execute("PRAGMA user_version").fetchone()[0]
            report["applicationId"] = conn.execute("PRAGMA application_id").fetchone()[0]
            table_rows = conn.execute("SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY type, name").fetchall()
            objects = []
            for row in table_rows:
                name = row["name"]
                quoted = name.replace('"', '""')
                columns = conn.execute(f'PRAGMA table_info("{quoted}")').fetchall()
                objects.append({"name": name, "type": row["type"], "columns": [{"name": col["name"], "type": col["type"], "notNull": bool(col["notnull"]), "primaryKey": bool(col["pk"])} for col in columns]})
            report["objects"] = objects
        finally:
            conn.close()
    except (OSError, sqlite3.Error) as exc:
        report["error"] = type(exc).__name__
    return report


def json_shape(value: Any, depth: int = 0) -> Any:
    if depth >= 4:
        return {"type": type(value).__name__}
    if isinstance(value, dict):
        return {"type": "object", "keys": {str(key)[:120]: json_shape(child, depth + 1) for key, child in sorted(value.items(), key=lambda item: str(item[0]))}}
    if isinstance(value, list):
        return {"type": "array", "length": len(value), "itemTypes": sorted({type(item).__name__ for item in value})}
    if value is None:
        return {"type": "null"}
    if isinstance(value, bool):
        return {"type": "boolean"}
    if isinstance(value, (int, float)):
        return {"type": "number"}
    if isinstance(value, str):
        return {"type": "string", "length": len(value)}
    return {"type": type(value).__name__}


def json_metadata(path: Path) -> dict[str, Any]:
    report: dict[str, Any] = {"path": display_path(path)}
    try:
        raw = path.read_text(encoding="utf-8")
        report["bytes"] = len(raw.encode("utf-8"))
        report["shape"] = json_shape(json.loads(raw))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        report["error"] = type(exc).__name__
    return report


def existing_roots(candidates: Iterable[Path], extras: Iterable[str]) -> list[Path]:
    roots: list[Path] = []
    seen: set[Path] = set()
    for candidate in [*candidates, *(Path(value).expanduser() for value in extras)]:
        try:
            normalized = candidate.expanduser().resolve()
        except OSError:
            normalized = candidate.expanduser()
        if normalized in seen or not normalized.exists():
            continue
        seen.add(normalized)
        roots.append(normalized)
    return roots


def default_zcode_roots() -> list[Path]:
    home = Path.home()
    values = [home / ".zcode", home / ".config" / "zcode", home / "Library" / "Application Support" / "ZCode"]
    for env_name in ("APPDATA", "LOCALAPPDATA"):
        if os.getenv(env_name):
            values.append(Path(os.environ[env_name]) / "ZCode")
    return values


def default_commandcode_roots() -> list[Path]:
    home = Path.home()
    values = [home / ".commandcode", home / ".config" / "commandcode", home / "Library" / "Application Support" / "CommandCode"]
    for env_name in ("APPDATA", "LOCALAPPDATA"):
        if os.getenv(env_name):
            values.append(Path(os.environ[env_name]) / "CommandCode")
    return values


def build_report(zcode_extra: Iterable[str] = (), commandcode_extra: Iterable[str] = ()) -> dict[str, Any]:
    zcode_roots = existing_roots(default_zcode_roots(), zcode_extra)
    command_roots = existing_roots(default_commandcode_roots(), commandcode_extra)
    zcode_dbs: list[Path] = []
    for root in zcode_roots:
        zcode_dbs.extend(_walk_limited(root, {".sqlite", ".sqlite3", ".db"}))
    zcode_dbs = list(dict.fromkeys(zcode_dbs))[:MAX_DATABASES]
    command_json: list[Path] = []
    for root in command_roots:
        for candidate_name in ("auth.json", "config.json", "settings.json"):
            candidate = root / candidate_name
            if candidate.is_file():
                command_json.append(candidate)
    return {
        "reportVersion": REPORT_VERSION,
        "generatedAt": datetime.now(timezone.utc).isoformat(),
        "privacy": {"networkCalls": False, "sqliteRowsRead": False, "jsonValuesIncluded": False, "hostnameIncluded": False, "usernameIncluded": False},
        "platform": {"system": platform.system(), "release": platform.release(), "machine": platform.machine(), "python": platform.python_version()},
        "zcode": {"cli": safe_cli_version(("zcode",)), "roots": [display_path(path) for path in zcode_roots], "databases": [sqlite_schema(path) for path in zcode_dbs]},
        "commandCode": {"cli": safe_cli_version(("commandcode", "command-code", "cmdcode")), "roots": [display_path(path) for path in command_roots], "jsonFiles": [json_metadata(path) for path in command_json]},
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a metadata-only discovery report for AI Control HUD.")
    parser.add_argument("--zcode-root", action="append", default=[], help="Additional ZCode root to inspect (repeatable).")
    parser.add_argument("--commandcode-root", action="append", default=[], help="Additional CommandCode root to inspect (repeatable).")
    parser.add_argument("--output", default=".local/discovery-report.json", help="Output path (default is gitignored).")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report = build_report(args.zcode_root, args.commandcode_root)
    output = Path(args.output).expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Wrote sanitized discovery report to {output}")
    print("Review the report before sharing it; no network calls or database row reads were performed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
