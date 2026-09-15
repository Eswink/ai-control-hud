from __future__ import annotations

import json
from pathlib import Path

import pytest
from pydantic import ValidationError

from server.hud.models import HudState

FIXTURE_DIR = Path(__file__).parent / "fixtures"


@pytest.mark.parametrize("path", sorted(FIXTURE_DIR.glob("*.json")), ids=lambda p: p.name)
def test_all_committed_fixtures_validate(path: Path) -> None:
    state = HudState.model_validate(json.loads(path.read_text(encoding="utf-8")))
    dumped = state.model_dump(mode="json", by_alias=True)
    assert dumped["schemaVersion"] == 1


def test_future_schema_is_rejected() -> None:
    payload = json.loads((FIXTURE_DIR / "healthy.json").read_text(encoding="utf-8"))
    payload["schemaVersion"] = 2
    with pytest.raises(ValidationError):
        HudState.model_validate(payload)


def test_live_requires_both_sources_ok() -> None:
    payload = json.loads((FIXTURE_DIR / "healthy.json").read_text(encoding="utf-8"))
    payload["commandCode"]["health"]["status"] = "stale"
    with pytest.raises(ValidationError, match="overall.status"):
        HudState.model_validate(payload)


def test_error_source_cannot_fake_empty_data() -> None:
    payload = json.loads((FIXTURE_DIR / "healthy.json").read_text(encoding="utf-8"))
    payload["zcode"]["health"] = {"status": "error", "observedAt": "2026-09-15T21:30:00+08:00", "lastSuccessAt": None, "message": "source unavailable"}
    payload["zcode"]["summary"] = {"running": 0, "waiting": 0, "failed": 0, "completed": 0}
    payload["zcode"]["tasks"] = []
    payload["overall"]["status"] = "degraded"
    with pytest.raises(ValidationError, match="must not expose untrusted data"):
        HudState.model_validate(payload)
