from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from server.hud.zcode_provider import default_zcode_provider_config, sanitized_provider_summary


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate a secret-free summary of ZCode model providers for AI Control HUD."
    )
    parser.add_argument(
        "--config",
        default=str(default_zcode_provider_config()),
        help="ZCode v2 config path (default: ~/.zcode/v2/config.json or HUD_ZCODE_CONFIG).",
    )
    parser.add_argument(
        "--output",
        default=".local/provider-discovery.json",
        help="Output path (default is gitignored).",
    )
    return parser.parse_args(argv)


def build_report(config: Path) -> dict:
    return {
        "reportVersion": 1,
        "generatedAt": datetime.now(timezone.utc).isoformat(),
        "privacy": {
            "networkCalls": False,
            "apiKeyValuesIncluded": False,
            "headerValuesIncluded": False,
            "promptOrTaskContentIncluded": False,
            "usernameIncluded": False,
        },
        "zcodeProviderConfig": sanitized_provider_summary(config),
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report = build_report(Path(args.config).expanduser())
    output = Path(args.output).expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Wrote sanitized provider report to {output}")
    print("Review before sharing. API keys and header values are never written to this report.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
