from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

from server.hud.zcode_provider import (
    default_zcode_provider_configs,
    sanitized_provider_summaries,
)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate a secret-free summary of ZCode model providers for AI Control HUD."
    )
    parser.add_argument(
        "--config",
        action="append",
        default=None,
        help=(
            "ZCode provider config path. Repeatable. Default scans both "
            "~/.zcode/v2/config.json and ~/.zcode/cli/config.json."
        ),
    )
    parser.add_argument(
        "--output",
        default=".local/provider-discovery.json",
        help="Output path (default is gitignored).",
    )
    return parser.parse_args(argv)


def build_report(configs: list[Path] | tuple[Path, ...]) -> dict:
    summaries = sanitized_provider_summaries(configs)
    candidates = []
    for summary in summaries:
        for provider in summary.get("providers", []):
            if provider.get("commandCodeCandidate"):
                candidates.append(
                    {
                        "sourcePath": summary.get("path"),
                        "providerId": provider.get("providerId"),
                        "name": provider.get("name"),
                        "kind": provider.get("kind"),
                        "baseURL": provider.get("baseURL"),
                        "host": provider.get("host"),
                        "apiKeyPresent": provider.get("apiKeyPresent"),
                        "modelIds": provider.get("modelIds", []),
                    }
                )
    return {
        "reportVersion": 2,
        "generatedAt": datetime.now(timezone.utc).isoformat(),
        "privacy": {
            "networkCalls": False,
            "apiKeyValuesIncluded": False,
            "headerValuesIncluded": False,
            "promptOrTaskContentIncluded": False,
            "usernameIncluded": False,
        },
        "zcodeProviderConfigs": summaries,
        "commandCodeCandidates": candidates,
    }


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    configs = (
        [Path(value).expanduser() for value in args.config]
        if args.config
        else list(default_zcode_provider_configs())
    )
    report = build_report(configs)
    output = Path(args.output).expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Wrote sanitized provider report to {output}")
    print("Scanned ZCode desktop/CLI provider configs. API keys and header values are never written.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
