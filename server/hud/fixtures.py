from __future__ import annotations

import json
from pathlib import Path

from .models import HudState

DEFAULT_FIXTURE = "healthy.json"
FIXTURE_DIR = Path(__file__).resolve().parents[1] / "tests" / "fixtures"


def load_fixture(name_or_path: str | Path = DEFAULT_FIXTURE) -> HudState:
    path = Path(name_or_path)
    if not path.is_absolute() and len(path.parts) == 1:
        path = FIXTURE_DIR / path
    payload = json.loads(path.read_text(encoding="utf-8"))
    return HudState.model_validate(payload)
