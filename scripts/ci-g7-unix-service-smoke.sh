#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENT="$REPO_ROOT/agent/ai-control-agent"
STATE_TOOL="$REPO_ROOT/scripts/g4_state_db.py"
PORT=18789
BASE_URL="http://127.0.0.1:$PORT"
ROOT="${RUNNER_TEMP:-/tmp}/ai-control-hud-g7-service-smoke"
SMOKE_HOME="$ROOT/home"
PROVIDER_ROOT="$ROOT/provider"
STATE_ROOT="$ROOT/state"
RUNTIME_DB="$SMOKE_HOME/.zcode/cli/db/db.sqlite"
TASK_INDEX_DB="$SMOKE_HOME/.zcode/v2/tasks-index.sqlite"
PROVIDER_CONFIG="$PROVIDER_ROOT/provider.json"
CONFIG_PATH="$STATE_ROOT/agent.json"

case "$(uname -s)" in
  Linux) ADAPTER="$REPO_ROOT/scripts/ai-control-agent-systemd.sh" ;;
  Darwin) ADAPTER="$REPO_ROOT/scripts/ai-control-agent-launchd.sh" ;;
  *) echo "unsupported smoke OS" >&2; exit 1 ;;
esac

rm -rf "$ROOT"
mkdir -p "$SMOKE_HOME" "$PROVIDER_ROOT" "$STATE_ROOT"
python3 "$STATE_TOOL" init --runtime "$RUNTIME_DB" --task-index "$TASK_INDEX_DB"
cat >"$PROVIDER_CONFIG" <<'JSON'
{
  "provider": {
    "command": {
      "name": "command",
      "kind": "openai",
      "enabled": true,
      "options": {
        "baseURL": "https://api.commandcode.ai/provider/v1",
        "apiKey": "test-only"
      },
      "models": {}
    }
  }
}
JSON

wait_state() {
  python3 - "$BASE_URL" <<'PY'
import json
import sys
import time
import urllib.request

base = sys.argv[1]
deadline = time.monotonic() + 30
last = None
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(base + "/api/v1/state", timeout=2) as response:
            last = json.load(response)
        if last.get("schemaVersion") == 1 and last.get("zcode", {}).get("health", {}).get("status") == "ok":
            print(json.dumps({
                "version": last.get("server", {}).get("version"),
                "zcode": last.get("zcode", {}).get("health", {}).get("status"),
                "running": (last.get("zcode", {}).get("summary") or {}).get("running"),
            }, sort_keys=True))
            raise SystemExit(0)
    except Exception:
        pass
    time.sleep(0.25)
print(json.dumps(last, sort_keys=True) if last else "<no state>", file=sys.stderr)
raise SystemExit(1)
PY
}

installed=0
cleanup() {
  if [[ "$installed" -eq 1 ]]; then
    bash "$ADAPTER" remove --config "$CONFIG_PATH" --purge || true
  fi
  rm -rf "$ROOT" || true
}
trap cleanup EXIT

echo "[g7-ci] install $(uname -s) service using caller-home ZCode discovery"
HOME="$SMOKE_HOME" bash "$ADAPTER" install \
  --agent "$AGENT" \
  --provider-config "$PROVIDER_CONFIG" \
  --listen "127.0.0.1:$PORT" \
  --config "$CONFIG_PATH"
installed=1

sudo "$AGENT" doctor --config "$CONFIG_PATH"
configured_runtime="$(sudo "$AGENT" config get --config "$CONFIG_PATH" --field zcode-runtime)"
configured_task_index="$(sudo "$AGENT" config get --config "$CONFIG_PATH" --field zcode-task-index)"
[[ "$configured_runtime" == "$RUNTIME_DB" ]] || {
  echo "runtime DB discovery mismatch: $configured_runtime" >&2
  exit 1
}
[[ "$configured_task_index" == "$TASK_INDEX_DB" ]] || {
  echo "task-index discovery mismatch: $configured_task_index" >&2
  exit 1
}
secret_path="$(sudo "$AGENT" config get --config "$CONFIG_PATH" --field command-code-secret)"
if [[ "$(uname -s)" == "Darwin" ]]; then
  mode="$(sudo stat -f '%Lp' "$secret_path")"
else
  mode="$(sudo stat -c '%a' "$secret_path")"
fi
[[ "$mode" == "600" ]] || { echo "unexpected secret mode $mode" >&2; exit 1; }

echo "[g7-ci] start service"
bash "$ADAPTER" start >/dev/null
wait_state

echo "[g7-ci] restart service"
bash "$ADAPTER" restart >/dev/null
wait_state

echo "[g7-ci] stop service"
bash "$ADAPTER" stop

echo "[g7-ci] remove service preserving state"
bash "$ADAPTER" remove --config "$CONFIG_PATH"
installed=0
[[ -f "$CONFIG_PATH" ]] || { echo "machine config disappeared" >&2; exit 1; }
[[ -f "$secret_path" ]] || { echo "SecretStore disappeared" >&2; exit 1; }

rm -f "$PROVIDER_CONFIG"
echo "[g7-ci] reinstall from preserved SecretStore without provider flag"
HOME="$SMOKE_HOME" bash "$ADAPTER" install \
  --agent "$AGENT" \
  --config "$CONFIG_PATH"
installed=1
sudo "$AGENT" doctor --config "$CONFIG_PATH"
preserved_listen="$(sudo "$AGENT" config get --config "$CONFIG_PATH" --field listen)"
[[ "$preserved_listen" == "127.0.0.1:$PORT" ]] || {
  echo "listen setting changed during reinstall: $preserved_listen" >&2
  exit 1
}
bash "$ADAPTER" start >/dev/null
wait_state
bash "$ADAPTER" stop

echo "[g7-ci] $(uname -s) service + SecretStore + home-discovery + reinstall smoke PASSED"