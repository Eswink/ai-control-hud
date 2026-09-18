#!/usr/bin/env python3
"""Enforce the Android UI resource/i18n and low-resource layout contract."""
from __future__ import annotations

import re
import sys
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
ANDROID_MAIN = ROOT / "android" / "app" / "src" / "main"
RES = ANDROID_MAIN / "res"
MAIN_ACTIVITY = ANDROID_MAIN / "java" / "dev" / "eswink" / "aicontrolhud" / "MainActivity.java"
DISPLAY_POLICY = ANDROID_MAIN / "java" / "dev" / "eswink" / "aicontrolhud" / "DisplayPolicy.java"
DESK_DISPLAY_SWITCH = ANDROID_MAIN / "java" / "dev" / "eswink" / "aicontrolhud" / "DeskDisplaySwitch.java"
MANIFEST = ANDROID_MAIN / "AndroidManifest.xml"
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


def check_landscape_focus() -> None:
    path = RES / "layout-land" / "activity_main.xml"
    if not path.is_file():
        fail("landscape focus layout is missing")
    root = ET.parse(path).getroot()
    ids = {
        value.removeprefix("@+id/").removeprefix("@id/")
        for element in root.iter()
        if (value := element.attrib.get(ANDROID_NS + "id"))
    }
    required = {
        "setupPanel",
        "dashboardPanel",
        "serverUrlInput",
        "connectButton",
        "setupStatus",
        "serverLabel",
        "liveStatus",
        "changeServerButton",
        "commandHealth",
        "planText",
        "creditText",
        "fiveHourLabel",
        "fiveHourProgress",
        "weeklyLabel",
        "weeklyProgress",
        "zcodeHealth",
        "zcodeSummary",
        "taskList",
        "taskEmpty",
        "taskOverflow",
        "lastUpdateText",
        "voiceStatusText",
        "completedVoiceSwitch",
        "failedVoiceSwitch",
        "quietHoursSwitch",
        "testVoiceButton",
        "deskDisplaySwitch",
    }
    missing = sorted(required - ids)
    if missing:
        fail("landscape focus layout is missing bound views: " + ", ".join(missing))

    text = path.read_text(encoding="utf-8")
    for required_string in (
        "@string/focus_command_remaining",
        "@string/focus_current_task",
        "@string/focus_tts_status",
        "@string/desk_display_keep_awake",
    ):
        if required_string not in text:
            fail(f"landscape focus layout is missing {required_string}")
    if "dev.eswink.aicontrolhud.DeskDisplaySwitch" not in text:
        fail("landscape focus layout must use the scoped DeskDisplaySwitch")


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
        "FLAG_KEEP_SCREEN_ON": "Activity must not keep the screen on unconditionally",
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


def check_manifest_rotation() -> None:
    root = ET.parse(MANIFEST).getroot()
    for activity in root.findall("./application/activity"):
        if activity.attrib.get(ANDROID_NS + "name") != ".MainActivity":
            continue
        if ANDROID_NS + "screenOrientation" in activity.attrib:
            fail("MainActivity must not be locked to one orientation once layout-land exists")
        return
    fail("MainActivity is missing from AndroidManifest.xml")


def check_keep_awake_policy() -> None:
    if not DISPLAY_POLICY.is_file() or not DESK_DISPLAY_SWITCH.is_file():
        fail("keep-awake policy and switch implementation are required")

    policy = DISPLAY_POLICY.read_text(encoding="utf-8")
    if "windowVisible && keepAwakeEnabled" not in policy:
        fail("keep-awake policy must require visible + enabled")
    if "landscape" in policy:
        fail("keep-awake policy must work in both portrait and landscape")

    switch = DESK_DISPLAY_SWITCH.read_text(encoding="utf-8")
    required_tokens = (
        "getWindowVisibility() == View.VISIBLE",
        "DisplayPolicy.shouldKeepScreenOn(windowVisible, isChecked())",
        "onDetachedFromWindow()",
        "setKeepScreenOn(false)",
    )
    for token in required_tokens:
        if token not in switch:
            fail(f"DeskDisplaySwitch is missing lifecycle guard {token!r}")
    if "Configuration.ORIENTATION_LANDSCAPE" in switch:
        fail("keep-awake switch must not be orientation-scoped")
    if "FLAG_KEEP_SCREEN_ON" in switch:
        fail("DeskDisplaySwitch must not set a permanent Window keep-screen-on flag")

    for layout in (RES / "layout" / "activity_main.xml", RES / "layout-land" / "activity_main.xml"):
        text = layout.read_text(encoding="utf-8")
        if "dev.eswink.aicontrolhud.DeskDisplaySwitch" not in text:
            fail(f"{layout.relative_to(ROOT)} must expose the keep-awake switch")
        if "@string/desk_display_keep_awake" not in text:
            fail(f"{layout.relative_to(ROOT)} must label the keep-awake switch")


def main() -> int:
    check_layouts()
    check_landscape_focus()
    check_catalogs()
    check_activity()
    check_manifest_rotation()
    check_keep_awake_policy()
    print(
        "[android-ui-resources] PASS localized-resources=literals-free landscape=focus "
        "lifecycle-guard=present keep-awake=foreground-opt-in"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
