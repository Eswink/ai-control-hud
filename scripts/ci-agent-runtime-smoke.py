from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


def read_json(url: str, timeout: float = 2.0) -> dict:
    with urllib.request.urlopen(url, timeout=timeout) as response:
        return json.load(response)


def wait_state(base_url: str, timeout: float = 20.0) -> dict:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    last_state: dict | None = None
    while time.monotonic() < deadline:
        try:
            last_state = read_json(f"{base_url}/api/v1/state")
            zcode = last_state.get("zcode", {})
            health = zcode.get("health", {})
            summary = zcode.get("summary") or {}
            if (
                last_state.get("schemaVersion") == 1
                and str(last_state.get("server", {}).get("version", "")).startswith("0.3.")
                and health.get("status") == "ok"
                and summary.get("running") == 1
            ):
                return last_state
        except (OSError, urllib.error.URLError, json.JSONDecodeError) as exc:
            last_error = exc
        time.sleep(0.2)
    diagnostic = json.dumps(last_state, ensure_ascii=False) if last_state else "<no state>"
    raise RuntimeError(f"agent runtime smoke timed out; last_error={last_error}; state={diagnostic}")


def assert_diagnostics(base_url: str, private_root: Path) -> dict:
    diagnostics = read_json(f"{base_url}/api/v1/diagnostics")
    if diagnostics.get("diagnosticsVersion") != 1:
        raise RuntimeError(f"unexpected diagnostics version: {diagnostics!r}")
    if diagnostics.get("stateSchemaVersion") != 1 or diagnostics.get("role") != "agent":
        raise RuntimeError(f"unexpected diagnostics identity: {diagnostics!r}")

    sources = diagnostics.get("sources") or {}
    zcode = sources.get("zcode") or {}
    if not zcode.get("enabled"):
        raise RuntimeError(f"zcode diagnostics unexpectedly disabled: {zcode!r}")
    if zcode.get("adapterKind") != "zcode.sqlite":
        raise RuntimeError(f"unexpected zcode adapter: {zcode!r}")
    if zcode.get("status") != "ok" or zcode.get("schemaSupport") != "supported":
        raise RuntimeError(f"unexpected zcode diagnostics: {zcode!r}")
    if zcode.get("lastSuccessAgeSeconds") is None:
        raise RuntimeError(f"zcode last-success age missing: {zcode!r}")

    command_code = sources.get("commandCode") or {}
    if command_code.get("enabled"):
        raise RuntimeError(f"commandcode diagnostics unexpectedly enabled: {command_code!r}")
    if command_code.get("schemaSupport") != "not-configured":
        raise RuntimeError(f"unexpected commandcode diagnostics: {command_code!r}")

    serialized = json.dumps(diagnostics, ensure_ascii=False)
    if str(private_root) in serialized:
        raise RuntimeError("diagnostics leaked a private filesystem path")
    return diagnostics


def stop_process(process: subprocess.Popen[str]) -> None:
    if process.poll() is not None:
        return
    if os.name == "nt":
        process.send_signal(signal.CTRL_BREAK_EVENT)
    else:
        process.send_signal(signal.SIGTERM)
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--agent", required=True, type=Path)
    parser.add_argument("--port", type=int, default=18788)
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parents[1]
    state_tool = repo_root / "scripts" / "g4_state_db.py"
    agent = args.agent.resolve()
    if not agent.is_file():
        raise SystemExit(f"agent binary not found: {agent}")

    with tempfile.TemporaryDirectory(prefix="ai-control-hud-native-") as temp_text:
        temp = Path(temp_text)
        runtime_db = temp / "runtime.sqlite"
        task_index_db = temp / "tasks-index.sqlite"
        empty_provider = temp / "provider.json"
        empty_provider.write_text("{}\n", encoding="utf-8")

        subprocess.run(
            [
                sys.executable,
                str(state_tool),
                "init",
                "--runtime",
                str(runtime_db),
                "--task-index",
                str(task_index_db),
            ],
            check=True,
        )

        env = os.environ.copy()
        env["HUD_ZCODE_RUNTIME_DB"] = str(runtime_db)
        env["HUD_ZCODE_DB"] = str(task_index_db)
        env["HUD_ZCODE_CONFIG"] = str(empty_provider)
        env.pop("AI_CONTROL_FIXTURE", None)
        env.pop("HUD_FIXTURE", None)
        env.pop("HUD_COMMANDCODE_PROVIDER_ID", None)

        listen = f"127.0.0.1:{args.port}"
        base_url = f"http://{listen}"
        creationflags = 0
        if os.name == "nt":
            creationflags = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
        process = subprocess.Popen(
            [str(agent), "run", "-listen", listen],
            cwd=repo_root,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            creationflags=creationflags,
        )
        try:
            state = wait_state(base_url)
            first = state["zcode"]["tasks"][0]
            if first.get("title") != "G4 synthetic Goal":
                raise RuntimeError(f"unexpected synthetic Goal title: {first.get('title')!r}")
            if first.get("activity") != "synthetic running activity":
                raise RuntimeError(f"unexpected synthetic activity: {first.get('activity')!r}")
            diagnostics = assert_diagnostics(base_url, temp)
            print(
                "[runtime-smoke] PASSED "
                f"platform={sys.platform} version={state['server']['version']} "
                f"zcode={state['zcode']['health']['status']} "
                f"diagnostics={diagnostics['diagnosticsVersion']}"
            )
        finally:
            stop_process(process)
            output = process.stdout.read() if process.stdout else ""
            if output:
                print(output.rstrip())

        if process.returncode not in (0, -signal.SIGTERM if os.name != "nt" else 0):
            raise RuntimeError(f"agent exited unexpectedly with code {process.returncode}")


if __name__ == "__main__":
    main()
