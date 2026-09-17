#!/usr/bin/env python3
"""Exercise a release Hub binary and reject obvious Linux resource leaks."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


class ResourceSmokeError(RuntimeError):
    pass


def available_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def proc_sample(pid: int) -> dict[str, int]:
    status = Path(f"/proc/{pid}/status")
    fd_dir = Path(f"/proc/{pid}/fd")
    if not status.is_file() or not fd_dir.is_dir():
        raise ResourceSmokeError("Linux /proc process metrics are unavailable")
    values: dict[str, int] = {}
    for line in status.read_text(encoding="utf-8").splitlines():
        if line.startswith("VmRSS:"):
            values["rss_kib"] = int(line.split()[1])
        elif line.startswith("Threads:"):
            values["threads"] = int(line.split()[1])
    values["fds"] = sum(1 for _ in fd_dir.iterdir())
    if "rss_kib" not in values or "threads" not in values:
        raise ResourceSmokeError("required /proc metrics are missing")
    return values


def read_health(opener: urllib.request.OpenerDirector, url: str, timeout: float = 2.0) -> None:
    with opener.open(url, timeout=timeout) as response:
        body = response.read(65537)
        if len(body) > 65536:
            raise ResourceSmokeError("health response exceeds size limit")
        payload = json.loads(body)
        if payload.get("status") != "ok" or payload.get("schemaVersion") != 1:
            raise ResourceSmokeError(f"unexpected health payload: {payload!r}")


def wait_ready(process: subprocess.Popen[bytes], opener: urllib.request.OpenerDirector, url: str) -> None:
    deadline = time.monotonic() + 10.0
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise ResourceSmokeError(f"Hub exited before readiness: {process.returncode}")
        try:
            read_health(opener, url, timeout=0.5)
            return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            time.sleep(0.05)
    raise ResourceSmokeError("Hub readiness timed out")


def run_smoke(binary: Path, requests: int) -> None:
    if sys.platform != "linux":
        raise ResourceSmokeError("resource smoke is Linux-only")
    binary = binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise ResourceSmokeError("Hub binary is missing or not executable")
    if requests < 100 or requests > 20_000:
        raise ResourceSmokeError("request count must be within 100..20000")

    port = available_port()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with tempfile.TemporaryDirectory(prefix="ai-control-hub-resource-") as root:
        env = os.environ.copy()
        env.update({
            "HUD_HUB_DB": str(Path(root) / "hub.sqlite3"),
            "HUD_HUB_AGENT_ID": "desktop-main",
            "HUD_HUB_AGENT_TOKEN": "0123456789abcdef" * 4,
            "HUD_HUB_STALE_AFTER_SECONDS": "45",
            "HUD_HUB_ID": "ci-resource-hub",
            "HUD_HUB_DISCOVERY_ENABLED": "0",
            "HUD_HUB_HTTP_SCHEME": "http",
            "HUD_HUB_HTTP_PORT": str(port),
        })
        log_path = Path(root) / "hub.log"
        with log_path.open("wb") as log:
            process = subprocess.Popen(
                [str(binary), "serve", "--host", "127.0.0.1", "--port", str(port)],
                env=env,
                stdout=log,
                stderr=subprocess.STDOUT,
            )
            try:
                url = f"http://127.0.0.1:{port}/api/v1/health"
                wait_ready(process, opener, url)
                before = proc_sample(process.pid)
                for _ in range(requests):
                    read_health(opener, url)
                time.sleep(0.25)
                after = proc_sample(process.pid)

                fd_growth = after["fds"] - before["fds"]
                thread_growth = after["threads"] - before["threads"]
                rss_growth_kib = after["rss_kib"] - before["rss_kib"]
                if fd_growth > 4:
                    raise ResourceSmokeError(f"file descriptor growth too high: {fd_growth}")
                if thread_growth > 4:
                    raise ResourceSmokeError(f"thread growth too high: {thread_growth}")
                if rss_growth_kib > 64 * 1024:
                    raise ResourceSmokeError(
                        f"RSS growth exceeds gross leak guard: {rss_growth_kib / 1024:.1f} MiB"
                    )

                print(
                    "[hub-resource-smoke] PASSED "
                    f"requests={requests} "
                    f"rss_before_kib={before['rss_kib']} rss_after_kib={after['rss_kib']} "
                    f"fds_before={before['fds']} fds_after={after['fds']} "
                    f"threads_before={before['threads']} threads_after={after['threads']}"
                )
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
                if process.returncode not in (0, -15):
                    log.flush()
                    try:
                        details = log_path.read_text(encoding="utf-8", errors="replace")
                    except OSError:
                        details = ""
                    raise ResourceSmokeError(
                        f"Hub process ended unexpectedly: exit={process.returncode}\n{details[-4000:]}"
                    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path)
    parser.add_argument("--requests", type=int, default=1000)
    args = parser.parse_args()
    try:
        run_smoke(args.hub, args.requests)
    except (ResourceSmokeError, OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"[hub-resource-smoke] FAILED {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
