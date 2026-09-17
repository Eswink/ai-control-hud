"""Negative controls for the same lifecycle gate invoked by Go Hub CI."""
from __future__ import annotations

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SMOKE = ROOT / "scripts" / "ci-hub-runtime-smoke.py"
FIXTURE = r'''
import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
import signal
import sys

mode = os.environ["HUD_TEST_HUB_MODE"]
if mode == "early":
    raise SystemExit(23)
if mode == "hang":
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
else:
    def stop(signum, frame):
        raise SystemExit(17 if mode == "bad-exit" else 0)
    signal.signal(signal.SIGTERM, stop)

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = {
            "schemaVersion": True if mode == "bad-schema" else 1,
            "status": "ok",
            "sources": {"zcode": "error", "commandCode": "error"},
        }
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(body).encode())

    def log_message(self, *args):
        pass

port = int(sys.argv[sys.argv.index("--port") + 1])
HTTPServer(("127.0.0.1", port), Handler).serve_forever()
'''


@unittest.skipUnless(os.name == "posix", "Hub deployment smoke uses POSIX SIGTERM")
class HubRuntimeSmokeTests(unittest.TestCase):
    def run_fixture(self, mode: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as root:
            binary = Path(root) / "hub-fixture"
            binary.write_text(f"#!{sys.executable}\n" + FIXTURE, encoding="utf-8")
            binary.chmod(0o755)
            env = os.environ.copy()
            env["HUD_TEST_HUB_MODE"] = mode
            return subprocess.run(
                [sys.executable, str(SMOKE), "--hub", str(binary),
                 "--startup-timeout", "3", "--shutdown-timeout", "0.5"],
                env=env, capture_output=True, text=True, timeout=10,
            )

    def test_clean_shutdown_passes(self):
        result = self.run_fixture("clean")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("shutdown=clean", result.stdout)

    def test_ready_but_nonzero_shutdown_fails(self):
        result = self.run_fixture("bad-exit")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("exit=17", result.stderr)
        self.assertNotIn("PASSED", result.stdout)

    def test_exit_before_readiness_fails(self):
        result = self.run_fixture("early")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("exit=23", result.stderr)

    def test_shutdown_timeout_fails(self):
        result = self.run_fixture("hang")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("shutdown timed out", result.stderr)

    def test_boolean_schema_is_not_integer_one(self):
        result = self.run_fixture("bad-schema")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("schemaVersion", result.stderr)


if __name__ == "__main__":
    unittest.main()
