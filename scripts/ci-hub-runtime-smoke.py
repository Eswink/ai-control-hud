#!/usr/bin/env python3
"""Probe an isolated Hub process and require a bounded, clean shutdown."""
from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


class SmokeError(RuntimeError):
    """An observable Hub lifecycle contract failed."""


def positive_seconds(value: str) -> float:
    seconds = float(value)
    if not math.isfinite(seconds) or seconds <= 0:
        raise argparse.ArgumentTypeError("timeout must be finite and positive")
    return seconds


def available_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def validate_health(body: object) -> None:
    if not isinstance(body, dict):
        raise SmokeError("health response must be an object")
    if type(body.get("schemaVersion")) is not int or body["schemaVersion"] != 1:
        raise SmokeError("health schemaVersion must be integer 1")
    if body.get("status") != "ok":
        raise SmokeError("empty Hub must report healthy service status")
    sources = body.get("sources")
    if not isinstance(sources, dict) or any(
        sources.get(name) != "error" for name in ("zcode", "commandCode")
    ):
        raise SmokeError("empty Hub must not fabricate healthy source data")


def run_smoke(binary: Path, startup_timeout: float, shutdown_timeout: float) -> None:
    binary = binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise SmokeError("Hub binary is missing or not executable")
    port = available_port()
    # Ignore inherited proxy settings: the smoke talks only to its loopback child.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with tempfile.TemporaryDirectory(prefix="ai-control-hub-runtime-") as root:
        env = os.environ.copy()
        env.update({
            "HUD_HUB_DB": str(Path(root) / "hub.sqlite3"),
            "HUD_HUB_AGENT_ID": "desktop-main",
            "HUD_HUB_AGENT_TOKEN": "0123456789abcdef" * 4,
            "HUD_HUB_STALE_AFTER_SECONDS": "45",
            "HUD_HUB_ID": "ci-hub",
            "HUD_HUB_DISCOVERY_ENABLED": "0",
            "HUD_HUB_DISCOVERY_PORT": "8788",
            "HUD_HUB_HTTP_SCHEME": "http",
            "HUD_HUB_HTTP_PORT": str(port),
        })
        with (Path(root) / "hub.log").open("wb") as log:
            process = subprocess.Popen(
                [str(binary), "serve", "--host", "127.0.0.1", "--port", str(port)],
                env=env, stdout=log, stderr=subprocess.STDOUT,
            )
            try:
                deadline = time.monotonic() + startup_timeout
                while True:
                    rc = process.poll()
                    if rc is not None:
                        raise SmokeError(f"Hub exited before readiness: exit={rc}")
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise SmokeError("Hub readiness timed out")
                    try:
                        with opener.open(
                            f"http://127.0.0.1:{port}/api/v1/health",
                            timeout=min(1.0, remaining),
                        ) as response:
                            data = response.read(65537)
                            if len(data) > 65536:
                                raise SmokeError("health response exceeds size limit")
                            validate_health(json.loads(data))
                        break
                    except (urllib.error.URLError, TimeoutError):
                        time.sleep(min(0.1, max(0.0, deadline - time.monotonic())))
                # A successful health request is not the end of the lifecycle gate.
                if process.poll() is not None:
                    raise SmokeError("Hub exited immediately after readiness")
                process.terminate()
                try:
                    rc = process.wait(timeout=shutdown_timeout)
                except subprocess.TimeoutExpired as exc:
                    raise SmokeError("Hub graceful shutdown timed out") from exc
                if rc != 0:
                    raise SmokeError(f"Hub did not shut down cleanly: exit={rc}")
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=5)
    print("[hub-runtime-smoke] PASSED health=schema-v1 shutdown=clean")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path)
    parser.add_argument("--startup-timeout", type=positive_seconds, default=10.0)
    parser.add_argument("--shutdown-timeout", type=positive_seconds, default=10.0)
    args = parser.parse_args()
    try:
        run_smoke(args.hub, args.startup_timeout, args.shutdown_timeout)
    except (SmokeError, OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"[hub-runtime-smoke] FAILED {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
