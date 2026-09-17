#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENT="$REPO_ROOT/agent/ai-control-agent"
STATE_TOOL="$REPO_ROOT/scripts/g4_state_db.py"
PORT=18789
BASE_URL="http://127.0.0.1:$PORT"
ROOT="${RUNNER_TEMP:-/tmp}/ai-control-hud-g7-service-smoke"
SMOKE_HOME="$ROOT/home"
STATE_ROOT="$ROOT/state"
RUNTIME_DB="$SMOKE_HOME/.zcode/cli/db/db.sqlite"
TASK_INDEX_DB="$SMOKE_HOME/.zcode/v2/tasks-index.sqlite"
API_KEY_FILE="$ROOT/commandcode.key"
HUB_TOKEN_FILE="$ROOT/hub.token"
CONFIG_PATH="$STATE_ROOT/agent.json"
HUB_SECRET_PATH="$STATE_ROOT/hub.dpapi"
INSTALLED_AGENT="/usr/local/lib/ai-control-hud/ai-control-agent"
BROKEN_AGENT="$ROOT/broken-agent"

case "$(uname -s)" in
  Linux)
    ADAPTER="$REPO_ROOT/scripts/ai-control-agent-systemd.sh"
    OUTBOX_PATH="/var/lib/ai-control-hud/agent/events.sqlite3"
    ;;
  Darwin)
    ADAPTER="$REPO_ROOT/scripts/ai-control-agent-launchd.sh"
    OUTBOX_PATH="$STATE_ROOT/events.sqlite3"
    ;;
  *) echo "unsupported smoke OS" >&2; exit 1 ;;
esac

rm -rf "$ROOT"
mkdir -p "$SMOKE_HOME" "$STATE_ROOT"
python3 "$STATE_TOOL" init --runtime "$RUNTIME_DB" --task-index "$TASK_INDEX_DB"
printf '%s\n' 'test-only' >"$API_KEY_FILE"
printf '%s\n' '0123456789abcdef0123456789abcdef' >"$HUB_TOKEN_FILE"
chmod 0600 "$API_KEY_FILE" "$HUB_TOKEN_FILE"
go build -o "$BROKEN_AGENT" "$REPO_ROOT/scripts/fixtures/broken-service-agent.go"
chmod 0755 "$BROKEN_AGENT"

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

hash_file() {
  sudo python3 - "$1" <<'PY'
import hashlib
import pathlib
import sys
print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest())
PY
}

assert_preserved_hashes() {
  [[ "$(hash_file "$CONFIG_PATH")" == "$CONFIG_HASH" ]] || { echo "machine config changed during binary upgrade" >&2; exit 1; }
  [[ "$(hash_file "$secret_path")" == "$COMMANDCODE_HASH" ]] || { echo "CommandCode SecretStore changed during binary upgrade" >&2; exit 1; }
  [[ "$(hash_file "$HUB_SECRET_PATH")" == "$HUB_HASH" ]] || { echo "Hub SecretStore changed during binary upgrade" >&2; exit 1; }
}

assert_outbox_exists() {
  sudo test -f "$OUTBOX_PATH" || { echo "durable Hub event outbox missing: $OUTBOX_PATH" >&2; exit 1; }
}

assert_no_upgrade_scratch() {
  sudo test ! -e "$INSTALLED_AGENT.upgrade.new" || { echo "staged upgrade file remains" >&2; exit 1; }
  sudo test ! -e "$INSTALLED_AGENT.upgrade.bak" || { echo "upgrade backup remains" >&2; exit 1; }
}

assert_service_inactive() {
  case "$(uname -s)" in
    Linux)
      state="$(systemctl is-active ai-control-agent.service 2>/dev/null || true)"
      [[ "$state" == "inactive" ]] || { echo "systemd service unexpectedly active after stopped-state upgrade: $state" >&2; exit 1; }
      ;;
    Darwin)
      if sudo launchctl print system/com.aicontrolhud.agent >/dev/null 2>&1; then
        echo "launchd service unexpectedly loaded after stopped-state upgrade" >&2
        exit 1
      fi
      ;;
  esac
}

installed=0
cleanup() {
  if sudo test -e "$HUB_SECRET_PATH" 2>/dev/null; then
    sudo "$AGENT" hub remove --config "$CONFIG_PATH" >/dev/null 2>&1 || true
  fi
  if [[ "$installed" -eq 1 ]]; then
    bash "$ADAPTER" remove --config "$CONFIG_PATH" --purge || true
  fi
  sudo rm -rf "$ROOT" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[g7-ci] import operator-supplied CommandCode key into protected Unix SecretStore"
sudo "$AGENT" commandcode configure \
  --config "$CONFIG_PATH" \
  --api-key-file "$API_KEY_FILE"
sudo "$AGENT" commandcode status --config "$CONFIG_PATH" | grep -Fq 'provider=command-code'

echo "[g7-ci] configure synthetic protected Hub credential"
sudo "$AGENT" hub configure \
  --config "$CONFIG_PATH" \
  --hub-auto \
  --hub-agent-id desktop-main \
  --hub-token-file "$HUB_TOKEN_FILE"
sudo test -f "$HUB_SECRET_PATH"

echo "[g7-ci] install $(uname -s) service using caller-home ZCode discovery and existing SecretStore"
HOME="$SMOKE_HOME" bash "$ADAPTER" install \
  --agent "$AGENT" \
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

CONFIG_HASH="$(hash_file "$CONFIG_PATH")"
COMMANDCODE_HASH="$(hash_file "$secret_path")"
HUB_HASH="$(hash_file "$HUB_SECRET_PATH")"

echo "[g7-ci] delete operator plaintext credentials after protected-store validation"
rm -f "$API_KEY_FILE" "$HUB_TOKEN_FILE"
[[ ! -e "$API_KEY_FILE" && ! -e "$HUB_TOKEN_FILE" ]] || { echo "plaintext credential remains" >&2; exit 1; }
sudo "$AGENT" commandcode status --config "$CONFIG_PATH" | grep -Fq 'provider=command-code'

echo "[g7-ci] start service"
bash "$ADAPTER" start >/dev/null
wait_state
assert_outbox_exists

echo "[g7-ci] restart service"
bash "$ADAPTER" restart >/dev/null
wait_state
assert_outbox_exists

echo "[g7-ci] running-state transactional upgrade"
bash "$ADAPTER" upgrade --agent "$AGENT"
wait_state
assert_preserved_hashes
assert_outbox_exists
assert_no_upgrade_scratch

echo "[g7-ci] broken candidate must roll back to previous running Agent"
if bash "$ADAPTER" upgrade --agent "$BROKEN_AGENT"; then
  echo "broken candidate unexpectedly upgraded successfully" >&2
  exit 1
fi
wait_state
assert_preserved_hashes
assert_outbox_exists
assert_no_upgrade_scratch

echo "[g7-ci] stop service"
bash "$ADAPTER" stop

echo "[g7-ci] stopped-state transactional upgrade must remain stopped"
bash "$ADAPTER" upgrade --agent "$AGENT"
assert_service_inactive
assert_preserved_hashes
assert_outbox_exists
assert_no_upgrade_scratch

echo "[g7-ci] remove service preserving state"
bash "$ADAPTER" remove --config "$CONFIG_PATH"
installed=0
[[ -f "$CONFIG_PATH" ]] || { echo "machine config disappeared" >&2; exit 1; }
[[ -f "$secret_path" ]] || { echo "SecretStore disappeared" >&2; exit 1; }
[[ -f "$HUB_SECRET_PATH" ]] || { echo "Hub SecretStore disappeared" >&2; exit 1; }
assert_outbox_exists

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
assert_preserved_hashes
assert_outbox_exists
bash "$ADAPTER" start >/dev/null
wait_state
assert_outbox_exists
bash "$ADAPTER" stop

echo "[g7-ci] final purge removes service-owned outbox while Hub credential remains independent"
sudo "$AGENT" hub remove --config "$CONFIG_PATH"
sudo test ! -e "$HUB_SECRET_PATH"
bash "$ADAPTER" remove --config "$CONFIG_PATH" --purge
installed=0
sudo test ! -e "$OUTBOX_PATH"
sudo test ! -e "$OUTBOX_PATH-wal"
sudo test ! -e "$OUTBOX_PATH-shm"

echo "[g7-ci] $(uname -s) service + manual-key SecretStore + durable outbox + transactional-upgrade + reinstall smoke PASSED"
