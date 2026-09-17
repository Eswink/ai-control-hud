from __future__ import annotations

import argparse
import copy
import json
from pathlib import Path
from typing import Any

CURRENT_PROVIDER_REPORT_VERSION = 2
SUPPORTED_PROVIDER_REPORT_VERSIONS = frozenset({1, 2})


class ProviderReportError(ValueError):
    """Base error for malformed provider-discovery reports."""


class UnsupportedProviderReportVersion(ProviderReportError):
    def __init__(self, version: int) -> None:
        super().__init__(f"unsupported provider reportVersion: {version}")
        self.version = version


def load_provider_report(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ProviderReportError("provider report could not be read") from exc
    return normalize_provider_report(value)


def normalize_provider_report(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ProviderReportError("provider report root must be an object")

    version = value.get("reportVersion")
    if isinstance(version, bool) or not isinstance(version, int):
        raise ProviderReportError("provider reportVersion must be an integer")
    if version not in SUPPORTED_PROVIDER_REPORT_VERSIONS:
        raise UnsupportedProviderReportVersion(version)

    if version == 1:
        return _normalize_v1(value)
    return _normalize_v2(value)


def _privacy(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ProviderReportError("provider privacy metadata must be an object")
    return copy.deepcopy(value)


def _normalize_v1(value: dict[str, Any]) -> dict[str, Any]:
    legacy_config = value.get("zcodeProviderConfig")
    if not isinstance(legacy_config, dict):
        raise ProviderReportError("provider report v1 requires zcodeProviderConfig")

    normalized = copy.deepcopy(value)
    privacy = _privacy(normalized.get("privacy"))
    privacy.setdefault("settingValuesIncluded", False)
    privacy.setdefault("sessionIdsIncluded", False)
    normalized["privacy"] = privacy

    # v1 predates multi-config discovery, explicit CommandCode candidates and
    # runtime SQLite evidence. Preserve what v1 actually knew and mark newer
    # semantic sections unknown instead of guessing them from legacy fields.
    normalized["zcodeProviderConfigs"] = [copy.deepcopy(legacy_config)]
    normalized["commandCodeCandidates"] = None
    normalized["runtimeDbEvidence"] = None
    return normalized


def _normalize_v2(value: dict[str, Any]) -> dict[str, Any]:
    configs = value.get("zcodeProviderConfigs")
    candidates = value.get("commandCodeCandidates")
    evidence = value.get("runtimeDbEvidence")
    if not isinstance(configs, list):
        raise ProviderReportError("provider report v2 requires zcodeProviderConfigs array")
    if not isinstance(candidates, list):
        raise ProviderReportError("provider report v2 requires commandCodeCandidates array")
    if evidence is not None and not isinstance(evidence, dict):
        raise ProviderReportError("provider report v2 runtimeDbEvidence must be object or null")

    normalized = copy.deepcopy(value)
    normalized["privacy"] = _privacy(normalized.get("privacy"))
    return normalized


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate an AI Control HUD provider-discovery report version."
    )
    parser.add_argument("report", type=Path)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        report = load_provider_report(args.report)
    except ProviderReportError as exc:
        print(f"provider report rejected: {exc}")
        return 2

    source_version = report["reportVersion"]
    print(
        "provider report supported: "
        f"reportVersion={source_version} current={CURRENT_PROVIDER_REPORT_VERSION}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
