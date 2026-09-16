from __future__ import annotations

import json
from pathlib import Path

import pytest

from tools.discovery import REPORT_VERSION
from tools.discovery_report import (
    CURRENT_DISCOVERY_REPORT_VERSION,
    InvalidDiscoveryReport,
    UnsupportedDiscoveryReportVersion,
    load_report,
    normalize_report,
    require_supported_version,
)


def test_generator_version_matches_consumer_contract() -> None:
    assert REPORT_VERSION == CURRENT_DISCOVERY_REPORT_VERSION == 2


def test_v1_report_is_explicitly_supported_and_normalized() -> None:
    report = {
        "reportVersion": 1,
        "privacy": {
            "networkCalls": False,
            "sqliteRowsRead": False,
            "jsonValuesIncluded": False,
            "hostnameIncluded": False,
            "usernameIncluded": False,
        },
        "zcode": {"cli": None, "roots": [], "databases": []},
        "commandCode": {"cli": None, "roots": [], "jsonFiles": []},
    }

    loaded = normalize_report(report)

    assert loaded.source_version == 1
    assert loaded.data["reportVersion"] == 1
    assert loaded.data["privacy"]["networkCallsByDiscoveryScript"] is False
    assert loaded.data["privacy"]["commandCodeCliMayUseNetwork"] is False
    assert loaded.data["privacy"]["sqliteContentFieldsRead"] is False
    assert loaded.data["privacy"]["environmentValuesIncluded"] is False
    assert loaded.data["privacy"]["commandCodeStatusValuesIncluded"] is False
    assert "networkCalls" not in loaded.data["privacy"]
    assert loaded.data["zcode"]["taskValueSummary"] is None
    assert loaded.data["commandCode"]["environment"] == {}
    assert loaded.data["commandCode"]["statusProbe"] is None

    # Normalization is copy-on-read and never mutates the original evidence report.
    assert "networkCalls" in report["privacy"]
    assert "taskValueSummary" not in report["zcode"]


def test_v2_report_preserves_forward_compatible_optional_fields() -> None:
    report = {
        "reportVersion": 2,
        "privacy": {"networkCallsByDiscoveryScript": False},
        "zcode": {"futureOptionalField": {"shape": "metadata-only"}},
        "commandCode": {},
        "futureTopLevel": True,
    }

    loaded = normalize_report(report)

    assert loaded.source_version == 2
    assert loaded.data == report
    assert loaded.data is not report


def test_future_report_version_is_rejected_explicitly() -> None:
    with pytest.raises(UnsupportedDiscoveryReportVersion) as exc:
        require_supported_version({"reportVersion": 3})

    assert exc.value.version == 3
    assert "supported versions: 1, 2" in str(exc.value)


@pytest.mark.parametrize(
    "payload",
    [
        {},
        {"reportVersion": "2"},
        {"reportVersion": True},
        {"reportVersion": None},
    ],
)
def test_missing_or_non_integer_version_is_invalid(payload: dict) -> None:
    with pytest.raises(InvalidDiscoveryReport):
        require_supported_version(payload)


def test_load_report_rejects_non_object_and_malformed_json(tmp_path: Path) -> None:
    array_report = tmp_path / "array.json"
    array_report.write_text(json.dumps([{"reportVersion": 2}]), encoding="utf-8")
    with pytest.raises(InvalidDiscoveryReport, match="root must be an object"):
        load_report(array_report)

    malformed = tmp_path / "malformed.json"
    malformed.write_text("{not-json", encoding="utf-8")
    with pytest.raises(InvalidDiscoveryReport, match="could not be read"):
        load_report(malformed)


def test_load_report_accepts_v1_file_with_normalized_view(tmp_path: Path) -> None:
    path = tmp_path / "discovery-report.json"
    path.write_text(
        json.dumps(
            {
                "reportVersion": 1,
                "privacy": {"networkCalls": False},
                "zcode": {},
                "commandCode": {},
            }
        ),
        encoding="utf-8",
    )

    loaded = load_report(path)

    assert loaded.source_version == 1
    assert loaded.data["privacy"]["networkCallsByDiscoveryScript"] is False
    assert loaded.data["zcode"]["taskValueSummary"] is None
    assert loaded.data["commandCode"]["statusProbe"] is None
