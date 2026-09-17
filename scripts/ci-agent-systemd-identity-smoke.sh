#!/usr/bin/env bash
set -euo pipefail

ROOT_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADAPTER="$ROOT_REPO/scripts/ai-control-agent-systemd.sh"
AGENT="$ROOT_REPO/agent/ai-control-agent"
STATE_TOOL="$ROOT_REPO/scripts/g4_state_db.py"
TMP="${RUNNER_TEMP:-/tmp}/ai-control-hud-h29-systemd-identity"
HOME_ROOT="$TMP/home"
RUNTIME_DB="$HOME_ROOT/.zcode/cli/db/db.sqlite"
TASK_INDEX_DB="$HOME_ROOT/.zcode/v2/tasks-index.sqlite"
NEW_SERVICE="ai-control-agent.service"
LEGACY_SERVICE="ai-control-hud.service"
NEW_UNIT="/etc/systemd/system/$NEW_SERVICE"
LEGACY_UNIT="/etc/systemd/system/$LEGACY_SERVICE"
INSTALL_DIR="/usr/local/lib/ai-control-hud"
INSTALLED_AGENT="$INSTALL_DIR/ai-control-agent"
AGENT_DATA_DIR="/var/lib/ai-control-hud/agent"
OUTBOX_PATH="$AGENT_DATA_DIR/events.sqlite3"
BROKEN_AGENT="$TMP/broken-agent"
CURRENT_CONFIG=""

rm -rf "$TMP"
mkdir -p "$HOME_ROOT"
python3 "$STATE_TOOL" init --runtime "$RUNTIME_DB" --task-index "$TASK_INDEX_DB"
go build -o "$BROKEN_AGENT" "$ROOT_REPO/scripts/fixtures/broken-service-agent.go"
chmod 0755 "$BROKEN_AGENT"

hash_root_file() {
  sudo python3 - "$1" <<'PY'
import hashlib
import pathlib
import sys
print(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest())
PY
}

wait_state() {
  local port="$1"
  python3 - "$port" <<'PY'
import json
import sys
import time
import urllib.request

port = int(sys.argv[1])
url = f"http://127.0.0.1:{port}/api/v1/state"
deadline = time.monotonic() + 30
last = None
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(url, timeout=2) as response:
            last = json.load(response)
        if last.get("schemaVersion") == 1 and last.get("zcode", {}).get("health", {}).get("status") == "ok":
            raise SystemExit(0)
    except Exception:
        pass
    time.sleep(0.25)
print(json.dumps(last, sort_keys=True) if last else "<no state>", file=sys.stderr)
raise SystemExit(1)
PY
}

remove_fixture_legacy_unit() {
  if sudo test -f "$LEGACY_UNIT" && sudo grep -Fq '# H29-CI-FIXTURE' "$LEGACY_UNIT"; then
    sudo systemctl stop "$LEGACY_SERVICE" 2>/dev/null || true
    sudo systemctl disable "$LEGACY_SERVICE" 2>/dev/null || true
    sudo rm -f "$LEGACY_UNIT"
    sudo systemctl daemon-reload
    sudo systemctl reset-failed "$LEGACY_SERVICE" 2>/dev/null || true
  fi
}

cleanup() {
  if [[ -n "$CURRENT_CONFIG" ]] && sudo test -e "$(dirname "$CURRENT_CONFIG")/hub.dpapi" 2>/dev/null; then
    sudo "$AGENT" hub remove --config "$CURRENT_CONFIG" >/dev/null 2>&1 || true
  fi
  if [[ -n "$CURRENT_CONFIG" ]]; then
    bash "$ADAPTER" remove --config "$CURRENT_CONFIG" --purge >/dev/null 2>&1 || true
  else
    bash "$ADAPTER" remove --purge >/dev/null 2>&1 || true
  fi
  remove_fixture_legacy_unit
  sudo rm -rf "$TMP" >/dev/null 2>&1 || true
}
trap cleanup EXIT

reset_agent_state() {
  local config="${1:-}"
  if [[ -n "$config" ]] && sudo test -e "$(dirname "$config")/hub.dpapi" 2>/dev/null; then
    sudo "$AGENT" hub remove --config "$config" >/dev/null 2>&1 || true
  fi
  if [[ -n "$config" ]]; then
    bash "$ADAPTER" remove --config "$config" --purge >/dev/null 2>&1 || true
  else
    bash "$ADAPTER" remove --purge >/dev/null 2>&1 || true
  fi
  remove_fixture_legacy_unit
  CURRENT_CONFIG=""
}

prepare_config() {
  local name="$1"
  local port="$2"
  local state_root="$TMP/$name-state"
  local config="$state_root/agent.json"
  local key_file="$TMP/$name-commandcode.key"
  local hub_file="$TMP/$name-hub.token"
  mkdir -p "$state_root"
  printf '%s\n' 'test-only' >"$key_file"
  printf '%s\n' '0123456789abcdef0123456789abcdef' >"$hub_file"
  chmod 0600 "$key_file" "$hub_file"

  sudo "$AGENT" commandcode configure --config "$config" --api-key-file "$key_file"
  sudo "$AGENT" hub configure \
    --config "$config" \
    --hub-auto \
    --hub-agent-id desktop-main \
    --hub-token-file "$hub_file"
  sudo "$AGENT" configure \
    --config "$config" \
    --runtime-db "$RUNTIME_DB" \
    --task-index-db "$TASK_INDEX_DB" \
    --listen "127.0.0.1:$port"
  rm -f "$key_file" "$hub_file"
  CURRENT_CONFIG="$config"
  printf '%s\n' "$config"
}

install_legacy_agent_binary_and_unit() {
  local config="$1"
  sudo install -d -m 0755 "$INSTALL_DIR"
  sudo install -m 0755 "$AGENT" "$INSTALLED_AGENT"
  sudo install -d -o root -g root -m 0700 "$AGENT_DATA_DIR"
  unit_tmp="$TMP/legacy-agent.service"
  cat >"$unit_tmp" <<EOF
# H29-CI-FIXTURE legacy-agent
[Unit]
Description=AI Control HUD historical Agent fixture
After=network-online.target

[Service]
Type=simple
Environment=AI_CONTROL_HUB_OUTBOX=$OUTBOX_PATH
ExecStart=$INSTALLED_AGENT run --config "$config"
Restart=on-failure
RestartSec=5s
ReadWritePaths=$AGENT_DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
  sudo install -m 0644 "$unit_tmp" "$LEGACY_UNIT"
  sudo systemctl daemon-reload
  sudo systemctl enable "$LEGACY_SERVICE"
}

echo "[h29-ci] non-Agent legacy-name Hub unit must be untouched"
config="$(prepare_config hub-preserve 18790)"
hub_unit_tmp="$TMP/hub-fixture.service"
cat >"$hub_unit_tmp" <<'EOF'
# H29-CI-FIXTURE hub
[Unit]
Description=AI Control HUD Central Hub fixture

[Service]
Type=simple
ExecStart=/usr/local/lib/ai-control-hub/ai-control-hub serve --host 127.0.0.1 --port 8787

[Install]
WantedBy=multi-user.target
EOF
sudo install -m 0644 "$hub_unit_tmp" "$LEGACY_UNIT"
sudo systemctl daemon-reload
sudo systemctl enable "$LEGACY_SERVICE"
hub_unit_hash="$(hash_root_file "$LEGACY_UNIT")"
HOME="$HOME_ROOT" bash "$ADAPTER" install --agent "$AGENT" --config "$config"
sudo test -f "$NEW_UNIT"
[[ "$(hash_root_file "$LEGACY_UNIT")" == "$hub_unit_hash" ]] || { echo "Hub unit changed during Agent install" >&2; exit 1; }
[[ "$(systemctl is-enabled "$LEGACY_SERVICE" 2>/dev/null || true)" == "enabled" ]] || { echo "Hub unit enable state changed during Agent install" >&2; exit 1; }
bash "$ADAPTER" remove --config "$config" --purge
CURRENT_CONFIG=""
[[ "$(hash_root_file "$LEGACY_UNIT")" == "$hub_unit_hash" ]] || { echo "Hub unit changed during Agent remove" >&2; exit 1; }
[[ "$(systemctl is-enabled "$LEGACY_SERVICE" 2>/dev/null || true)" == "enabled" ]] || { echo "Hub unit enable state changed during Agent remove" >&2; exit 1; }
remove_fixture_legacy_unit

echo "[h29-ci] inactive historical Agent migrates and stays inactive"
config="$(prepare_config legacy-inactive 18791)"
install_legacy_agent_binary_and_unit "$config"
config_hash="$(hash_root_file "$config")"
command_secret="$(sudo "$AGENT" config get --config "$config" --field command-code-secret)"
command_hash="$(hash_root_file "$command_secret")"
hub_hash="$(hash_root_file "$(dirname "$config")/hub.dpapi")"
HOME="$HOME_ROOT" bash "$ADAPTER" install --agent "$AGENT" --config "$config"
sudo test -f "$NEW_UNIT"
sudo test ! -e "$LEGACY_UNIT"
state="$(systemctl is-active "$NEW_SERVICE" 2>/dev/null || true)"
[[ "$state" == "inactive" ]] || { echo "inactive legacy Agent migration changed state: $state" >&2; exit 1; }
[[ "$(hash_root_file "$config")" == "$config_hash" ]] || { echo "config changed during inactive identity migration" >&2; exit 1; }
[[ "$(hash_root_file "$command_secret")" == "$command_hash" ]] || { echo "CommandCode secret changed during inactive identity migration" >&2; exit 1; }
[[ "$(hash_root_file "$(dirname "$config")/hub.dpapi")" == "$hub_hash" ]] || { echo "Hub secret changed during inactive identity migration" >&2; exit 1; }
reset_agent_state "$config"

echo "[h29-ci] active historical Agent rejects bad candidate and remains healthy"
config="$(prepare_config legacy-active 18792)"
install_legacy_agent_binary_and_unit "$config"
sudo systemctl start "$LEGACY_SERVICE"
wait_state 18792
sudo test -f "$OUTBOX_PATH"
config_hash="$(hash_root_file "$config")"
command_secret="$(sudo "$AGENT" config get --config "$config" --field command-code-secret)"
command_hash="$(hash_root_file "$command_secret")"
hub_path="$(dirname "$config")/hub.dpapi"
hub_hash="$(hash_root_file "$hub_path")"
old_binary_hash="$(hash_root_file "$INSTALLED_AGENT")"
if HOME="$HOME_ROOT" bash "$ADAPTER" install --agent "$BROKEN_AGENT" --config "$config"; then
  echo "broken migration candidate unexpectedly succeeded" >&2
  exit 1
fi
sudo test -f "$LEGACY_UNIT"
sudo test ! -e "$NEW_UNIT"
sudo systemctl is-active --quiet "$LEGACY_SERVICE"
wait_state 18792
[[ "$(hash_root_file "$INSTALLED_AGENT")" == "$old_binary_hash" ]] || { echo "old binary was not restored after failed migration candidate" >&2; exit 1; }
[[ "$(hash_root_file "$config")" == "$config_hash" ]] || { echo "config changed after failed migration candidate" >&2; exit 1; }
[[ "$(hash_root_file "$command_secret")" == "$command_hash" ]] || { echo "CommandCode secret changed after failed migration candidate" >&2; exit 1; }
[[ "$(hash_root_file "$hub_path")" == "$hub_hash" ]] || { echo "Hub secret changed after failed migration candidate" >&2; exit 1; }

echo "[h29-ci] active historical Agent migrates to dedicated Agent identity"
HOME="$HOME_ROOT" bash "$ADAPTER" install --agent "$AGENT" --config "$config"
sudo test -f "$NEW_UNIT"
sudo test ! -e "$LEGACY_UNIT"
sudo systemctl is-active --quiet "$NEW_SERVICE"
wait_state 18792
[[ "$(hash_root_file "$config")" == "$config_hash" ]] || { echo "config changed during active identity migration" >&2; exit 1; }
[[ "$(hash_root_file "$command_secret")" == "$command_hash" ]] || { echo "CommandCode secret changed during active identity migration" >&2; exit 1; }
[[ "$(hash_root_file "$hub_path")" == "$hub_hash" ]] || { echo "Hub secret changed during active identity migration" >&2; exit 1; }
sudo test -f "$OUTBOX_PATH"

bash "$ADAPTER" status >/dev/null
bash "$ADAPTER" stop
state="$(systemctl is-active "$NEW_SERVICE" 2>/dev/null || true)"
[[ "$state" == "inactive" ]] || { echo "dedicated Agent service did not stop: $state" >&2; exit 1; }
bash "$ADAPTER" upgrade --agent "$AGENT"
state="$(systemctl is-active "$NEW_SERVICE" 2>/dev/null || true)"
[[ "$state" == "inactive" ]] || { echo "dedicated Agent upgrade changed stopped state: $state" >&2; exit 1; }

sudo "$AGENT" hub remove --config "$config"
bash "$ADAPTER" remove --config "$config" --purge
CURRENT_CONFIG=""
sudo test ! -e "$NEW_UNIT"
sudo test ! -e "$LEGACY_UNIT"

echo "[h29-ci] Linux Agent systemd identity isolation + migration smoke PASSED"
