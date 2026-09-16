from __future__ import annotations

import copy

import pytest

from tools.provider_discovery import build_report
from tools.provider_report import (
    CURRENT_PROVIDER_REPORT_VERSION,
    ProviderReportError,
    UnsupportedProviderReportVersion,
    normalize_provider_report,
)


def test_current_generator_matches_consumer_version() -> None:
    generated = build_report([], None)
    assert generated["reportVersion"] == CURRENT_PROVIDER_REPORT_VERSION == 2
    normalized = normalize_provider_report(generated)
    assert normalized == generated


def test_v1_normalizes_without_guessing_new_semantics() -> None:
    original = {
        "reportVersion": 1,
        "generatedAt": "2026-09-15T00:00:00+00:00",
        "privacy": {
            "networkCalls": False,
            "apiKeyValuesIncluded": False,
            "headerValuesIncluded": False,
            "promptOrTaskContentIncluded": False,
            "usernameIncluded": False,
        },
        "zcodeProviderConfig": {
            "path": "~/.zcode/v2/config.json",
            "providers": [],
        },
    }
    before = copy.deepcopy(original)

    normalized = normalize_provider_report(original)

    assert original == before
    assert normalized["reportVersion"] == 1
    assert normalized["zcodeProviderConfigs"] == [original["zcodeProviderConfig"]]
    assert normalized["commandCodeCandidates"] is None
    assert normalized["runtimeDbEvidence"] is None
    assert normalized["privacy"]["settingValuesIncluded"] is False
    assert normalized["privacy"]["sessionIdsIncluded"] is False


def test_v2_requires_current_structural_sections() -> None:
    with pytest.raises(ProviderReportError, match="zcodeProviderConfigs"):
        normalize_provider_report(
            {
                "reportVersion": 2,
                "privacy": {},
                "commandCodeCandidates": [],
                "runtimeDbEvidence": None,
            }
        )

    with pytest.raises(ProviderReportError, match="commandCodeCandidates"):
        normalize_provider_report(
            {
                "reportVersion": 2,
                "privacy": {},
                "zcodeProviderConfigs": [],
                "runtimeDbEvidence": None,
            }
        )

    with pytest.raises(ProviderReportError, match="runtimeDbEvidence"):
        normalize_provider_report(
            {
                "reportVersion": 2,
                "privacy": {},
                "zcodeProviderConfigs": [],
                "commandCodeCandidates": [],
                "runtimeDbEvidence": "unknown",
            }
        )


def test_future_version_is_explicitly_rejected() -> None:
    with pytest.raises(UnsupportedProviderReportVersion) as raised:
        normalize_provider_report({"reportVersion": 3, "privacy": {}})
    assert raised.value.version == 3


@pytest.mark.parametrize(
    "value",
    [
        {},
        {"reportVersion": "2"},
        {"reportVersion": True},
        {"reportVersion": None},
        [],
    ],
)
def test_missing_or_non_integer_version_is_invalid(value) -> None:
    with pytest.raises(ProviderReportError):
        normalize_provider_report(value)
