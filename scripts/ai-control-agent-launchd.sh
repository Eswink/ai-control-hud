#!/usr/bin/env bash
set -euo pipefail

LABEL="com.aicontrolhud.agent"
INSTALL_DIR="/usr/local/lib/ai-control-hud"
CONFIG_PATH="/Library/Application Support/AI Control HUD/agent.json"
PLIST_PATH="/Library/LaunchDaemons/${LABEL}.plist"
LISTEN=""
AGENT=""
PROVIDER_CONFIG=""
RUNTIME_DB=""
TASK_INDEX_DB=""
PURGE=0

usage() {
  cat <<'EOF'
Usage:
  ai-control-agent-launchd.sh install --agent PATH [--provider-config PATH] [--runtime-db PATH] [--task-index-db PATH] [--listen HOST:PORT] [--config PATH]
  ai-control-agent-launchd.sh start|stop|restart|status
  ai-control-agent-launchd.sh remove [--purge] [--config PATH]
EOF
}

if [[ $# -lt 1 ]]; then
  usage >&2
  exit 2
fi
ACTION="$1"
shift

while [[ $# -gt 0 ]]; do
  case "$1" in
    --agent) AGENT="$2"; shift 2 ;;
    --provider-config) PROVIDER_CONFIG="$2"; shift 2 ;;
    --runtime-db) RUNTIME_DB="$2"; shift 2 ;;
    --task-index-db) TASK_INDEX_DB="$2"; shift 2 ;;
    --listen) LISTEN="$2"; shift 2 ;;
    --config) CONFIG_PATH="$2"; shift 2 ;;
    --purge) PURGE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

require_launchd() {
  [[ "$(uname -s)" == "Darwin" ]] || { echo "launchd adapter requires macOS" >&2; exit 1; }
  command -v launchctl >/dev/null 2>&1 || { echo "launchctl is unavailable" >&2; exit 1; }
}

loaded() {
  sudo launchctl print "system/$LABEL" >/dev/null 2>&1
}

secret_from_config() {
  if ! sudo test -f "$CONFIG_PATH"; then
    return 0
  fi
  sudo python3 - "$CONFIG_PATH" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    value = json.load(handle).get("commandCodeSecret", "")
if value:
    print(value)
PY
}

case "$ACTION" in
  install)
    require_launchd
    [[ -n "$AGENT" && -f "$AGENT" ]] || { echo "--agent must point to the agent binary" >&2; exit 2; }
    agent_abs="$(cd "$(dirname "$AGENT")" && pwd)/$(basename "$AGENT")"
    existing_config=0
    if sudo test -f "$CONFIG_PATH"; then
      existing_config=1
    fi

    sudo install -d -m 0755 "$INSTALL_DIR"
    sudo install -m 0755 "$agent_abs" "$INSTALL_DIR/ai-control-agent"

    configure=(sudo "$INSTALL_DIR/ai-control-agent" configure --config "$CONFIG_PATH")
    if [[ -n "$LISTEN" ]]; then
      configure+=(--listen "$LISTEN")
    elif [[ "$existing_config" -eq 0 ]]; then
      configure+=(--listen "0.0.0.0:8787")
    fi
    [[ -n "$PROVIDER_CONFIG" ]] && configure+=(--provider-config "$PROVIDER_CONFIG")
    [[ -n "$RUNTIME_DB" ]] && configure+=(--runtime-db "$RUNTIME_DB")
    [[ -n "$TASK_INDEX_DB" ]] && configure+=(--task-index-db "$TASK_INDEX_DB")
    "${configure[@]}"

    plist_tmp="$(mktemp)"
    trap 'rm -f "$plist_tmp"' EXIT
    python3 - "$plist_tmp" "$INSTALL_DIR/ai-control-agent" "$CONFIG_PATH" "$LABEL" <<'PY'
import plistlib
import sys

output, agent, config, label = sys.argv[1:]
payload = {
    "Label": label,
    "ProgramArguments": [agent, "run", "--config", config],
    "RunAtLoad": True,
    "KeepAlive": {"SuccessfulExit": False},
    "ProcessType": "Background",
    "ThrottleInterval": 5,
}
with open(output, "wb") as handle:
    plistlib.dump(payload, handle, fmt=plistlib.FMT_XML, sort_keys=True)
PY
    sudo install -m 0644 "$plist_tmp" "$PLIST_PATH"
    sudo plutil -lint "$PLIST_PATH"
    echo "[launchd] installed label=$LABEL config=$CONFIG_PATH"
    ;;
  start)
    require_launchd
    if loaded; then
      sudo launchctl kickstart -k "system/$LABEL"
    else
      sudo launchctl bootstrap system "$PLIST_PATH"
    fi
    sleep 1
    sudo launchctl print "system/$LABEL"
    ;;
  stop)
    require_launchd
    if loaded; then
      sudo launchctl bootout "system/$LABEL"
    fi
    ;;
  restart)
    require_launchd
    if loaded; then
      sudo launchctl kickstart -k "system/$LABEL"
    else
      sudo launchctl bootstrap system "$PLIST_PATH"
    fi
    sleep 1
    sudo launchctl print "system/$LABEL"
    ;;
  status)
    require_launchd
    sudo launchctl print "system/$LABEL"
    ;;
  remove)
    require_launchd
    secret_path="$(secret_from_config || true)"
    if loaded; then
      sudo launchctl bootout "system/$LABEL" || true
    fi
    sudo rm -f "$PLIST_PATH"
    if [[ "$PURGE" -eq 1 ]]; then
      [[ -z "$secret_path" ]] || sudo rm -f "$secret_path"
      sudo rm -f "$CONFIG_PATH"
      sudo rmdir "$(dirname "$CONFIG_PATH")" 2>/dev/null || true
      sudo rm -f "$INSTALL_DIR/ai-control-agent"
      sudo rmdir "$INSTALL_DIR" 2>/dev/null || true
      echo "[launchd] removed and purged"
    else
      echo "[launchd] removed; config/SecretStore and installed binary preserved"
    fi
    ;;
  *)
    echo "Unknown action: $ACTION" >&2
    usage >&2
    exit 2
    ;;
esac
