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

REPORT_VERSION = 2
MAX_DATABASES = 100
MAX_SCAN_DEPTH = 6
COMMANDCODE_CLI_NAMES = ("cmdc", "command-code", "commandcode", "cmdcode")


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


def _find_executable(names: Iterable[str]) -> tuple[str, str] | None:
    for name in names:
        executable = shutil.which(name)
        if executable:
            return name, executable
    return None


def safe_cli_version(names: Iterable[str]) -> dict[str, Any] | None:
    found = _find_executable(names)
    if found is None:
        return None
    name, executable = found
    result: dict[str, Any] = {"name": name, "found": True, "path": display_path(Path(executable))}
    try:
        proc = subprocess.run(
            [executable, "--version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=3,
            env={**os.environ, "NO_COLOR": "1"},
        )
        combined = (proc.stdout or proc.stderr).strip().splitlines()
        result["versionOutput"] = combined[0][:200] if combined else None
        result["versionExitCode"] = proc.returncode
    except (OSError, subprocess.SubprocessError) as exc:
        result["versionError"] = type(exc).__name__
    return result


def safe_cli_json_shape(names: Iterable[str], args: list[str]) -> dict[str, Any] | None:
    found = _find_executable(names)
    if found is None:
        return None
    name, executable = found
    result: dict[str, Any] = {
        "name": name,
        "path": display_path(Path(executable)),
        "arguments": args,
        "valuesIncluded": False,
    }
    try:
        proc = subprocess.run(
            [executable, *args],
            check=False,
            capture_output=True,
            text=True,
            timeout=8,
            env={**os.environ, "NO_COLOR": "1"},
        )
        result["exitCode"] = proc.returncode
        raw = (proc.stdout or "").strip()
        if raw:
            try:
                result["shape"] = json_shape(json.loads(raw))
            except json.JSONDecodeError:
                result["stdout"] = {"type": "text", "length": len(raw)}
        stderr = (proc.stderr or "").strip()
        if stderr:
            result["stderr"] = {"type": "text", "length": len(stderr)}
    except (OSError, subprocess.SubprocessError) as exc:
        result["error"] = type(exc).__name__
    return result


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


def _sqlite_readonly(path: Path) -> sqlite3.Connection:
    uri = f"file:{quote(str(path.expanduser().resolve()))}?mode=ro"
    conn = sqlite3.connect(uri, uri=True, timeout=1)
    conn.row_factory = sqlite3.Row
    return conn


def sqlite_schema(path: Path) -> dict[str, Any]:
    report: dict[str, Any] = {"path": display_path(path), "readOnly": True}
    try:
        conn = _sqlite_readonly(path)
        try:
            report["userVersion"] = conn.execute("PRAGMA user_version").fetchone()[0]
            report["applicationId"] = conn.execute("PRAGMA application_id").fetchone()[0]
            table_rows = conn.execute(
                "SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') "
                "AND name NOT LIKE 'sqlite_%' ORDER BY type, name"
            ).fetchall()
            objects = []
            for row in table_rows:
                name = row["name"]
                quoted = name.replace('"', '""')
                columns = conn.execute(f'PRAGMA table_info("{quoted}")').fetchall()
                objects.append(
                    {
                        "name": name,
                        "type": row["type"],
                        "columns": [
                            {
                                "name": col["name"],
                                "type": col["type"],
                                "notNull": bool(col["notnull"]),
                                "primaryKey": bool(col["pk"]),
                            }
                            for col in columns
                        ],
                    }
                )
            report["objects"] = objects
        finally:
            conn.close()
    except (OSError, sqlite3.Error) as exc:
        report["error"] = type(exc).__name__
    return report


def _integer_digits(value: Any) -> int | None:
    if value is None:
        return None
    try:
        return len(str(abs(int(value))))
    except (TypeError, ValueError, OverflowError):
        return None


def _infer_timestamp_unit(max_digits: int | None) -> str | None:
    if max_digits is None:
        return None
    if max_digits <= 10:
        return "seconds"
    if max_digits <= 13:
        return "milliseconds"
    if max_digits <= 16:
        return "microseconds"
    if max_digits <= 19:
        return "nanoseconds"
    return "unknown"


def _timestamp_magnitude(count: int, minimum: Any, maximum: Any) -> dict[str, Any]:
    min_digits = _integer_digits(minimum)
    max_digits = _integer_digits(maximum)
    return {
        "count": int(count),
        "minDigits": min_digits,
        "maxDigits": max_digits,
        "inferredUnit": _infer_timestamp_unit(max_digits),
    }


def zcode_task_value_summary(path: Path) -> dict[str, Any]:
    """Read only aggregate, non-content task metadata from the ZCode task index."""
    report: dict[str, Any] = {
        "path": display_path(path),
        "readOnly": True,
        "contentFieldsRead": False,
    }
    required = {"task_status", "created_at", "updated_at", "archived", "deleted"}
    try:
        conn = _sqlite_readonly(path)
        try:
            table = conn.execute(
                "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'tasks'"
            ).fetchone()
            if table is None:
                report["error"] = "UnsupportedTaskSchema"
                return report
            columns = {row[1] for row in conn.execute('PRAGMA table_info("tasks")').fetchall()}
            if not required.issubset(columns):
                report["error"] = "UnsupportedTaskSchema"
                return report

            status_rows = conn.execute(
                """
                SELECT task_status, COUNT(*) AS count
                FROM tasks
                WHERE archived = 0 AND deleted = 0
                GROUP BY task_status
                ORDER BY count DESC, task_status
                """
            ).fetchall()
            timestamp_row = conn.execute(
                """
                SELECT COUNT(created_at) AS created_count,
                       MIN(created_at) AS created_min,
                       MAX(created_at) AS created_max,
                       COUNT(updated_at) AS updated_count,
                       MIN(updated_at) AS updated_min,
                       MAX(updated_at) AS updated_max
                FROM tasks
                WHERE archived = 0 AND deleted = 0
                """
            ).fetchone()
        finally:
            conn.close()
    except (OSError, sqlite3.Error) as exc:
        report["error"] = type(exc).__name__
        return report

    report["activeTaskCount"] = sum(int(row["count"]) for row in status_rows)
    report["statusCounts"] = [
        {"status": row["task_status"], "count": int(row["count"])}
        for row in status_rows
    ]
    report["timestamps"] = {
        "createdAt": _timestamp_magnitude(
            timestamp_row["created_count"],
            timestamp_row["created_min"],
            timestamp_row["created_max"],
        ),
        "updatedAt": _timestamp_magnitude(
            timestamp_row["updated_count"],
            timestamp_row["updated_min"],
            timestamp_row["updated_max"],
        ),
    }
    return report


def json_shape(value: Any, depth: int = 0) -> Any:
    if depth >= 4:
        return {"type": type(value).__name__}
    if isinstance(value, dict):
        return {
            "type": "object",
            "keys": {
                str(key)[:120]: json_shape(child, depth + 1)
                for key, child in sorted(value.items(), key=lambda item: str(item[0]))
            },
        }
    if isinstance(value, list):
        return {
            "type": "array",
            "length": len(value),
            "itemTypes": sorted({type(item).__name__ for item in value}),
        }
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
    values = [
        home / ".commandcode",
        home / ".config" / "commandcode",
        home / "Library" / "Application Support" / "CommandCode",
    ]
    for env_name in ("APPDATA", "LOCALAPPDATA"):
        if os.getenv(env_name):
            values.append(Path(os.environ[env_name]) / "CommandCode")
    return values


def environment_presence(names: Iterable[str]) -> dict[str, dict[str, bool]]:
    return {name: {"present": bool(os.getenv(name))} for name in names}


def _task_index_path(zcode_dbs: list[Path]) -> Path | None:
    for path in zcode_dbs:
        if path.name.lower() == "tasks-index.sqlite":
            return path
    return None


def build_report(
    zcode_extra: Iterable[str] = (),
    commandcode_extra: Iterable[str] = (),
    *,
    commandcode_status: bool = False,
    zcode_status_summary: bool = False,
) -> dict[str, Any]:
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

    command_cli = safe_cli_version(COMMANDCODE_CLI_NAMES)
    command_status = (
        safe_cli_json_shape(COMMANDCODE_CLI_NAMES, ["status", "--json"])
        if commandcode_status
        else None
    )
    task_index = _task_index_path(zcode_dbs)
    task_summary = (
        zcode_task_value_summary(task_index)
        if zcode_status_summary and task_index is not None
        else ({"error": "TaskIndexNotFound"} if zcode_status_summary else None)
    )

    return {
        "reportVersion": REPORT_VERSION,
        "generatedAt": datetime.now(timezone.utc).isoformat(),
        "privacy": {
            "networkCallsByDiscoveryScript": False,
            "commandCodeCliMayUseNetwork": bool(commandcode_status),
            "sqliteRowsRead": bool(zcode_status_summary),
            "sqliteContentFieldsRead": False,
            "jsonValuesIncluded": False,
            "environmentValuesIncluded": False,
            "hostnameIncluded": False,
            "usernameIncluded": False,
            "commandCodeStatusValuesIncluded": False,
        },
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "machine": platform.machine(),
            "python": platform.python_version(),
        },
        "zcode": {
            "cli": safe_cli_version(("zcode",)),
            "roots": [display_path(path) for path in zcode_roots],
            "databases": [sqlite_schema(path) for path in zcode_dbs],
            "taskValueSummary": task_summary,
        },
        "commandCode": {
            "cli": command_cli,
            "environment": environment_presence(("COMMAND_CODE_API_KEY",)),
            "roots": [display_path(path) for path in command_roots],
            "jsonFiles": [json_metadata(path) for path in command_json],
            "statusProbe": command_status,
        },
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a metadata-only discovery report for AI Control HUD.")
    parser.add_argument("--zcode-root", action="append", default=[], help="Additional ZCode root to inspect (repeatable).")
    parser.add_argument("--commandcode-root", action="append", default=[], help="Additional CommandCode root to inspect (repeatable).")
    parser.add_argument(
        "--commandcode-status",
        action="store_true",
        help="Invoke `cmdc status --json` (or equivalent) and record JSON shape only; the CLI may perform its own network checks.",
    )
    parser.add_argument(
        "--zcode-status-summary",
        action="store_true",
        help="Read aggregate active ZCode task status counts and timestamp magnitude only; no task content/IDs/paths are emitted.",
    )
    parser.add_argument("--output", default=".local/discovery-report.json", help="Output path (default is gitignored).")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report = build_report(
        args.zcode_root,
        args.commandcode_root,
        commandcode_status=args.commandcode_status,
        zcode_status_summary=args.zcode_status_summary,
    )
    output = Path(args.output).expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Wrote sanitized discovery report to {output}")
    print("Review the report before sharing it; JSON/environment secret values and ZCode content fields are not included.")
    if args.commandcode_status:
        print("Command Code status was invoked explicitly; its values were reduced to shape/type metadata only.")
    if args.zcode_status_summary:
        print("ZCode aggregate status/timestamp metadata was read explicitly; task titles, IDs, content and full paths were not queried.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
