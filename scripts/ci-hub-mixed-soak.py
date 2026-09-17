#!/usr/bin/env python3
"""Exercise Hub ingest/read/retention paths and reject obvious Linux resource leaks."""
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


class SoakError(RuntimeError):
    pass


def available_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def sample_proc(pid: int) -> dict[str, int]:
    status = Path(f"/proc/{pid}/status")
    fd_dir = Path(f"/proc/{pid}/fd")
    if not status.is_file() or not fd_dir.is_dir():
        raise SoakError("Linux /proc metrics unavailable")
    result: dict[str, int] = {}
    for line in status.read_text(encoding="utf-8").splitlines():
        if line.startswith("VmRSS:"):
            result["rss_kib"] = int(line.split()[1])
        elif line.startswith("Threads:"):
            result["threads"] = int(line.split()[1])
    result["fds"] = sum(1 for _ in fd_dir.iterdir())
    if "rss_kib" not in result or "threads" not in result:
        raise SoakError("required /proc metrics missing")
    return result


def request_json(
    opener: urllib.request.OpenerDirector,
    method: str,
    url: str,
    payload: object | None = None,
    token: str | None = None,
    timeout: float = 3.0,
) -> dict:
    data = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("Accept", "application/json")
    if data is not None:
        request.add_header("Content-Type", "application/json")
    if token is not None:
        request.add_header("Authorization", f"Bearer {token}")
    with opener.open(request, timeout=timeout) as response:
        body = response.read(2 * 1024 * 1024 + 1)
        if len(body) > 2 * 1024 * 1024:
            raise SoakError(f"response too large from {url}")
        if response.status < 200 or response.status >= 300:
            raise SoakError(f"unexpected HTTP {response.status} from {url}")
        return json.loads(body) if body else {}


def iso(base: float, offset: int = 0) -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(base + offset))


def healthy_state(epoch: float, serial: int) -> dict:
    observed = iso(epoch, serial)
    return {
        "schemaVersion": 1,
        "server": {"version": "agent-soak", "time": observed, "uptimeSeconds": serial + 1},
        "overall": {"status": "live"},
        "zcode": {
            "health": {"status": "ok", "observedAt": observed, "lastSuccessAt": observed, "message": None},
            "summary": {"running": 1, "waiting": 0, "failed": 0, "completed": serial},
            "tasks": [{
                "id": "soak-running",
                "title": "Soak running task",
                "workspace": "ci",
                "status": "running",
                "startedAt": iso(epoch),
                "updatedAt": observed,
                "durationSeconds": serial,
                "activity": "resource soak",
                "changes": {"additions": serial, "deletions": 0},
            }],
        },
        "commandCode": {
            "health": {"status": "ok", "observedAt": observed, "lastSuccessAt": observed, "message": None},
            "usage": {
                "plan": "ci",
                "credit": {"remaining": 50.0, "limit": 100.0, "unit": "credits"},
                "windows": [],
            },
        },
    }


def event_batch(epoch: float, start: int, count: int) -> list[dict]:
    values: list[dict] = []
    for i in range(start, start + count):
        values.append({
            "eventId": f"{i:032x}",
            "type": "task.completed",
            "occurredAt": iso(epoch, i),
            "task": {
                "id": f"soak-task-{i}",
                "title": f"Soak task {i}",
                "workspace": "ci",
                "status": "completed",
            },
        })
    return values


def wait_ready(process: subprocess.Popen[bytes], opener: urllib.request.OpenerDirector, base: str) -> None:
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise SoakError(f"Hub exited before readiness: {process.returncode}")
        try:
            health = request_json(opener, "GET", base + "/api/v1/health", timeout=0.5)
            if health.get("status") == "ok" and health.get("schemaVersion") == 1:
                return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            pass
        time.sleep(0.05)
    raise SoakError("Hub readiness timed out")


def stop_process(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    if process.returncode not in (0, -15):
        raise SoakError(f"Hub exited unexpectedly: {process.returncode}")


def start_hub(binary: Path, db: Path, port: int, token: str, log) -> subprocess.Popen[bytes]:
    env = os.environ.copy()
    env.update({
        "HUD_HUB_DB": str(db),
        "HUD_HUB_AGENT_ID": "desktop-main",
        "HUD_HUB_AGENT_TOKEN": token,
        "HUD_HUB_STALE_AFTER_SECONDS": "45",
        "HUD_HUB_ID": "ci-soak-hub",
        "HUD_HUB_DISCOVERY_ENABLED": "0",
        "HUD_HUB_HTTP_SCHEME": "http",
        "HUD_HUB_HTTP_PORT": str(port),
        "HUD_HUB_EVENT_RETENTION_DAYS": "90",
        "HUD_HUB_EVENT_RETENTION_MIN": "100",
        "HUD_HUB_EVENT_RETENTION_MAX": "1000",
        "HUD_HUB_MAINTENANCE_INTERVAL_HOURS": "6",
    })
    return subprocess.Popen(
        [str(binary), "serve", "--host", "127.0.0.1", "--port", str(port)],
        env=env,
        stdout=log,
        stderr=subprocess.STDOUT,
    )


def run_soak(binary: Path, events_count: int, reads: int, heartbeats: int) -> None:
    if sys.platform != "linux":
        raise SoakError("Hub mixed soak is Linux-only")
    binary = binary.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise SoakError("Hub binary missing or not executable")
    if events_count < 1000 or events_count > 10000 or events_count % 100 != 0:
        raise SoakError("events must be 1000..10000 and divisible by 100")
    if reads < 300 or reads > 10000 or heartbeats < 100 or heartbeats > 5000:
        raise SoakError("read/heartbeat workload outside allowed bounds")

    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    token = "0123456789abcdef" * 4
    epoch = 1_788_000_000.0
    with tempfile.TemporaryDirectory(prefix="ai-control-hub-mixed-soak-") as root:
        root_path = Path(root)
        db = root_path / "hub.sqlite3"
        log_path = root_path / "hub.log"
        port = available_port()
        base = f"http://127.0.0.1:{port}"
        with log_path.open("wb") as log:
            process = start_hub(binary, db, port, token, log)
            try:
                wait_ready(process, opener, base)
                request_json(opener, "POST", base + "/api/v1/agent/state", {
                    "agentId": "desktop-main", "sentAt": iso(epoch), "state": healthy_state(epoch, 0)
                }, token)
                # Warm JSON/SQLite paths before taking the baseline.
                for _ in range(50):
                    request_json(opener, "GET", base + "/api/v1/state")
                    request_json(opener, "GET", base + "/api/v1/health")
                before = sample_proc(process.pid)

                for i in range(heartbeats):
                    request_json(opener, "POST", base + "/api/v1/agent/heartbeat", {
                        "agentId": "desktop-main", "sentAt": iso(epoch, i), "agentVersion": "soak"
                    }, token)
                    if i % 50 == 0:
                        request_json(opener, "POST", base + "/api/v1/agent/state", {
                            "agentId": "desktop-main", "sentAt": iso(epoch, i), "state": healthy_state(epoch, i)
                        }, token)

                for start in range(1, events_count + 1, 100):
                    request_json(opener, "POST", base + "/api/v1/agent/events", {
                        "agentId": "desktop-main",
                        "sentAt": iso(epoch, start),
                        "events": event_batch(epoch, start, 100),
                    }, token)

                for i in range(reads):
                    path = "/api/v1/state" if i % 2 == 0 else "/api/v1/health"
                    request_json(opener, "GET", base + path)

                cursor = 0
                while True:
                    page = request_json(opener, "GET", base + f"/api/v1/events?after={cursor}&limit=100")
                    events = page.get("events") or []
                    if not events:
                        break
                    cursor = int(page["nextAfter"])
                    if cursor >= int(page["latestSeq"]):
                        break

                time.sleep(0.75)
                after = sample_proc(process.pid)
                fd_growth = after["fds"] - before["fds"]
                thread_growth = after["threads"] - before["threads"]
                rss_growth = after["rss_kib"] - before["rss_kib"]
                if fd_growth > 4:
                    raise SoakError(f"FD growth too high: {fd_growth}")
                if thread_growth > 4:
                    raise SoakError(f"thread growth too high: {thread_growth}")
                if rss_growth > 64 * 1024:
                    raise SoakError(f"RSS growth exceeds 64 MiB gross guard: {rss_growth / 1024:.1f} MiB")

                print(
                    "[hub-mixed-soak] phase1 PASSED "
                    f"events={events_count} reads={reads} heartbeats={heartbeats} "
                    f"rss_kib={before['rss_kib']}->{after['rss_kib']} "
                    f"fds={before['fds']}->{after['fds']} threads={before['threads']}->{after['threads']}"
                )
            finally:
                stop_process(process)

        # A restart performs an immediate retention pass. With max=1000 the
        # previously stored workload must converge to the configured hard cap.
        port2 = available_port()
        base2 = f"http://127.0.0.1:{port2}"
        with log_path.open("ab") as log:
            process2 = start_hub(binary, db, port2, token, log)
            try:
                wait_ready(process2, opener, base2)
                deadline = time.monotonic() + 10
                last = None
                while time.monotonic() < deadline:
                    last = request_json(opener, "GET", base2 + "/api/v1/events?after=0&limit=1")
                    oldest = int(last.get("oldestSeq", 0))
                    latest = int(last.get("latestSeq", 0))
                    if latest == events_count and oldest >= events_count - 1000 + 1:
                        print(f"[hub-mixed-soak] retention PASSED oldest={oldest} latest={latest}")
                        break
                    time.sleep(0.1)
                else:
                    raise SoakError(f"retention did not converge after restart: {last!r}")
            finally:
                stop_process(process2)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True, type=Path)
    parser.add_argument("--events", type=int, default=2000)
    parser.add_argument("--reads", type=int, default=1000)
    parser.add_argument("--heartbeats", type=int, default=500)
    args = parser.parse_args()
    try:
        run_soak(args.hub, args.events, args.reads, args.heartbeats)
    except (SoakError, OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"[hub-mixed-soak] FAILED {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
