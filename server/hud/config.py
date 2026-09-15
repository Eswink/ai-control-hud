from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Mapping


@dataclass(frozen=True)
class RuntimeConfig:
    zcode_interval_seconds: float = 1.0
    command_code_interval_seconds: float = 60.0

    def __post_init__(self) -> None:
        if self.zcode_interval_seconds <= 0:
            raise ValueError("zcode interval must be positive")
        if self.command_code_interval_seconds <= 0:
            raise ValueError("commandCode interval must be positive")

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "RuntimeConfig":
        values = os.environ if env is None else env
        return cls(
            zcode_interval_seconds=float(values.get("HUD_ZCODE_INTERVAL_SECONDS", "1")),
            command_code_interval_seconds=float(
                values.get("HUD_COMMANDCODE_INTERVAL_SECONDS", "60")
            ),
        )
