#!/usr/bin/env python3
"""Enforce the Android UI resource/i18n foundation without Android tooling."""
from __future__ import annotations

import re
import sys
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
RES = ROOT / "android" / "app" / "src" / "main" / "res"
MAIN_ACTIVITY = ROOT / "android" / "app" / "src" / "main" / "java" / "dev" / "eswink" / "aicontrolhud" / "MainActivity.java"
ANDROID_NS = "{http://schemas.android.com/apk/res/android}"


def fail(message: str) -> None:
    print(f"[android-ui-resources] FAIL {message}", file=sys.stderr)
    raise SystemExit(1)


def ui_names(values_dir: Path) -> set[str]:
    result: set[str] = set()
    if not values_dir.is_dir():
        return result
    for path in sorted(values_dir.glob("*.xml")):
        root = ET.parse(path).getroot()
        for child in root:
            if child.tag not in {"string", "plurals"}:
                continue
            name = child.attrib.get("name")
            if name:
                result.add(name)
    return result


def check_layouts() -> None:
    for layout_dir in sorted(p for p in RES.iterdir() if p.is_dir() and p.name.startswith("layout")):
        for path in sorted(layout_dir.glob("*.xml")):
            root = ET.parse(path).getroot()
            for element in root.iter():
                for attribute in ("text", "hint", "contentDescription"):
                    value = element.attrib.get(ANDROID_NS + attribute)
                    if value is None or value.startswith("@") or value.startswith("?"):
                        continue
                    fail(f"{path.relative_to(ROOT)} has hard-coded android:{attribute}={value!r}")


def check_catalogs() -> None:
    default = ui_names(RES / "values")
    zh_cn = ui_names(RES / "values-zh-rCN")
    if not default:
        fail("default UI string catalog is empty")
    missing = sorted(default - zh_cn)
    if missing:
        fail("zh-rCN is missing UI resources: " + ", ".join(missing))


def check_activity() -> None:
    text = MAIN_ACTIVITY.read_text(encoding="utf-8")
    banned = {
        "FLAG_KEEP_SCREEN_ON": "portrait Activity must not keep the screen on unconditionally",
        "Color.rgb(": "MainActivity colors must use resource tokens",
    }
    for token, reason in banned.items():
        if token in text:
            fail(f"MainActivity contains {token!r}: {reason}")

    if re.search(r"\.setText\(\s*\"[^\"]+\"\s*\)", text):
        fail("MainActivity contains a visible setText string literal")
    if "removeCallbacksAndMessages(null)" not in text or "networkExecutor.shutdownNow()" not in text:
        fail("MainActivity destruction path is missing callback/executor cleanup")
    if "postToUi(" not in text or "volatile boolean destroyed" not in text:
        fail("MainActivity is missing destroyed-Activity callback guard")


def main() -> int:
    check_layouts()
    check_catalogs()
    check_activity()
    print("[android-ui-resources] PASS localized-resources=literals-free lifecycle-guard=present")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
