from __future__ import annotations

import argparse
import copy
import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping

CURRENT_DISCOVERY_REPORT_VERSION = 2
SUPPORTED_DISCOVERY_REPORT_VERSIONS = frozenset({1, 2})


class DiscoveryReportError(ValueError):
    """Base error for discovery-report contract violations."""


class InvalidDiscoveryReport(DiscoveryReportError):
    """The payload is not a structurally valid discovery report."""


class UnsupportedDiscoveryReportVersion(DiscoveryReportError):
    def __init__(self, version: int):
        supported = ", ".join(str(value) for value in sorted(SUPPORTED_DISCOVERY_REPORT_VERSIONS))
        super().__init__(
            f"unsupported discovery report version {version}; supported versions: {supported}"
        )
        self.version = version


@dataclass(frozen=True)
class LoadedDiscoveryReport:
    source_version: int
    data: dict[str, Any]


def require_supported_version(report: Mapping[str, Any]) -> int:
    if "reportVersion" not in report:
        raise InvalidDiscoveryReport("discovery report is missing reportVersion")
    value = report["reportVersion"]
    if type(value) is not int:  # bool is intentionally rejected too.
        raise InvalidDiscoveryReport("discovery report reportVersion must be an integer")
    if value not in SUPPORTED_DISCOVERY_REPORT_VERSIONS:
        raise UnsupportedDiscoveryReportVersion(value)
    return value


def normalize_report(report: Mapping[str, Any]) -> LoadedDiscoveryReport:
    version = require_supported_version(report)
    normalized = copy.deepcopy(dict(report))
    if version == 1:
        _normalize_v1(normalized)
    return LoadedDiscoveryReport(source_version=version, data=normalized)


def _normalize_v1(report: dict[str, Any]) -> None:
    privacy_value = report.get("privacy")
    privacy = dict(privacy_value) if isinstance(privacy_value, Mapping) else {}
    legacy_network_calls = bool(privacy.pop("networkCalls", False))
    privacy.setdefault("networkCallsByDiscoveryScript", legacy_network_calls)
    privacy.setdefault("commandCodeCliMayUseNetwork", False)
    privacy.setdefault("sqliteRowsRead", False)
    privacy.setdefault("sqliteContentFieldsRead", False)
    privacy.setdefault("jsonValuesIncluded", False)
    privacy.setdefault("environmentValuesIncluded", False)
    privacy.setdefault("hostnameIncluded", False)
    privacy.setdefault("usernameIncluded", False)
    privacy.setdefault("commandCodeStatusValuesIncluded", False)
    report["privacy"] = privacy

    zcode_value = report.get("zcode")
    if isinstance(zcode_value, Mapping):
        zcode = dict(zcode_value)
        zcode.setdefault("taskValueSummary", None)
        report["zcode"] = zcode

    command_value = report.get("commandCode")
    if isinstance(command_value, Mapping):
        command_code = dict(command_value)
        command_code.setdefault("environment", {})
        command_code.setdefault("statusProbe", None)
        report["commandCode"] = command_code


def load_report(path: str | Path) -> LoadedDiscoveryReport:
    source = Path(path).expanduser()
    try:
        payload = json.loads(source.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise InvalidDiscoveryReport(
            f"discovery report could not be read: {type(exc).__name__}"
        ) from exc
    if not isinstance(payload, dict):
        raise InvalidDiscoveryReport("discovery report root must be an object")
    return normalize_report(payload)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate and normalize an AI Control HUD discovery report."
    )
    parser.add_argument("report", help="Path to discovery-report.json")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        loaded = load_report(args.report)
    except DiscoveryReportError as exc:
        print(f"discovery report rejected: {exc}", file=sys.stderr)
        return 2
    print(
        "discovery report accepted: "
        f"sourceVersion={loaded.source_version} "
        f"currentVersion={CURRENT_DISCOVERY_REPORT_VERSION}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
