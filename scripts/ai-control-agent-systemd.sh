#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UPGRADE_HELPER="$SCRIPT_DIR/ai-control-agent-unix-upgrade.sh"
SERVICE_NAME="ai-control-hud.service"
INSTALL_DIR="/usr/local/lib/ai-control-hud"
CONFIG_PATH="/etc/ai-control-hud/agent.json"
UNIT_PATH="/etc/systemd/system/${SERVICE_NAME}"
AGENT_DATA_DIR="/var/lib/ai-control-hud/agent"
OUTBOX_PATH="$AGENT_DATA_DIR/events.sqlite3"
LISTEN=""
AGENT=""
PROVIDER_CONFIG=""
RUNTIME_DB=""
TASK_INDEX_DB=""
PURGE=0

usage() {
  cat <<'EOF'
Usage:
  ai-control-agent-systemd.sh install --agent PATH [--provider-config PATH] [--runtime-db PATH] [--task-index-db PATH] [--listen HOST:PORT] [--config PATH]
  ai-control-agent-systemd.sh upgrade --agent PATH
  ai-control-agent-systemd.sh start|stop|restart|status
  ai-control-agent-systemd.sh remove [--purge] [--config PATH]
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

case "$CONFIG_PATH" in
  /*) ;;
  *) CONFIG_PATH="$PWD/$CONFIG_PATH" ;;
esac

require_systemd() {
  command -v systemctl >/dev/null 2>&1 || { echo "systemctl is unavailable" >&2; exit 1; }
  [[ -d /run/systemd/system ]] || { echo "systemd is not the active service manager" >&2; exit 1; }
}

caller_home() {
  if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]] && command -v getent >/dev/null 2>&1; then
    local resolved
    resolved="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
    if [[ -n "$resolved" ]]; then
      printf '%s\n' "$resolved"
      return
    fi
  fi
  printf '%s\n' "$HOME"
}

secret_from_config() {
  if ! sudo test -f "$CONFIG_PATH"; then
    return 0
  fi
  if sudo test -x "$INSTALL_DIR/ai-control-agent"; then
    sudo "$INSTALL_DIR/ai-control-agent" config get \
      --config "$CONFIG_PATH" \
      --field command-code-secret
  else
    printf '%s\n' "$(dirname "$CONFIG_PATH")/commandcode.dpapi"
  fi
}

case "$ACTION" in
  install)
    require_systemd
    [[ -n "$AGENT" && -f "$AGENT" ]] || { echo "--agent must point to the agent binary" >&2; exit 2; }
    agent_abs="$(cd "$(dirname "$AGENT")" && pwd)/$(basename "$AGENT")"
    config_abs="$CONFIG_PATH"
    existing_config=0
    if sudo test -f "$config_abs"; then
      existing_config=1
    fi

    if [[ "$existing_config" -eq 0 ]]; then
      home_hint="$(caller_home)"
      if [[ -z "$RUNTIME_DB" && -f "$home_hint/.zcode/cli/db/db.sqlite" ]]; then
        RUNTIME_DB="$home_hint/.zcode/cli/db/db.sqlite"
      fi
      if [[ -z "$TASK_INDEX_DB" && -f "$home_hint/.zcode/v2/tasks-index.sqlite" ]]; then
        TASK_INDEX_DB="$home_hint/.zcode/v2/tasks-index.sqlite"
      fi
      if [[ -z "$RUNTIME_DB" && -z "$TASK_INDEX_DB" ]]; then
        echo "No ZCode database found under $home_hint/.zcode; pass --runtime-db and/or --task-index-db" >&2
        exit 1
      fi
    fi

    sudo install -d -m 0755 "$INSTALL_DIR"
    sudo install -m 0755 "$agent_abs" "$INSTALL_DIR/ai-control-agent"
    sudo install -d -m 0755 "$(dirname "$AGENT_DATA_DIR")"
    sudo install -d -o root -g root -m 0700 "$AGENT_DATA_DIR"

    configure=(sudo "$INSTALL_DIR/ai-control-agent" configure --config "$config_abs")
    if [[ -n "$LISTEN" ]]; then
      configure+=(--listen "$LISTEN")
    elif [[ "$existing_config" -eq 0 ]]; then
      configure+=(--listen "0.0.0.0:8787")
    fi
    [[ -n "$PROVIDER_CONFIG" ]] && configure+=(--provider-config "$PROVIDER_CONFIG")
    [[ -n "$RUNTIME_DB" ]] && configure+=(--runtime-db "$RUNTIME_DB")
    [[ -n "$TASK_INDEX_DB" ]] && configure+=(--task-index-db "$TASK_INDEX_DB")
    "${configure[@]}"

    unit_tmp="$(mktemp)"
    trap 'rm -f "$unit_tmp"' EXIT
    cat >"$unit_tmp" <<EOF
[Unit]
Description=AI Control HUD Agent
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
Environment=AI_CONTROL_HUB_OUTBOX=$OUTBOX_PATH
ExecStart=$INSTALL_DIR/ai-control-agent run --config "$config_abs"
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
ReadWritePaths=$AGENT_DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
    sudo install -m 0644 "$unit_tmp" "$UNIT_PATH"
    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE_NAME"
    echo "[systemd] installed service=$SERVICE_NAME config=$config_abs outbox=$OUTBOX_PATH"
    ;;
  upgrade)
    require_systemd
    [[ -n "$AGENT" && -f "$AGENT" ]] || { echo "--agent must point to the replacement agent binary" >&2; exit 2; }
    [[ -f "$UPGRADE_HELPER" ]] || { echo "Unix upgrade helper is missing next to adapter: $UPGRADE_HELPER" >&2; exit 1; }
    bash "$UPGRADE_HELPER" --manager systemd --agent "$AGENT"
    ;;
  start)
    require_systemd
    sudo systemctl start "$SERVICE_NAME"
    sudo systemctl --no-pager --full status "$SERVICE_NAME"
    ;;
  stop)
    require_systemd
    sudo systemctl stop "$SERVICE_NAME"
    ;;
  restart)
    require_systemd
    sudo systemctl restart "$SERVICE_NAME"
    sudo systemctl --no-pager --full status "$SERVICE_NAME"
    ;;
  status)
    require_systemd
    sudo systemctl --no-pager --full status "$SERVICE_NAME"
    ;;
  remove)
    require_systemd
    secret_path="$(secret_from_config || true)"
    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sudo systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    sudo rm -f "$UNIT_PATH"
    sudo systemctl daemon-reload
    sudo systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true
    if [[ "$PURGE" -eq 1 ]]; then
      [[ -z "$secret_path" ]] || sudo rm -f "$secret_path"
      sudo rm -f "$CONFIG_PATH"
      sudo rmdir "$(dirname "$CONFIG_PATH")" 2>/dev/null || true
      sudo rm -f "$OUTBOX_PATH" "$OUTBOX_PATH-wal" "$OUTBOX_PATH-shm"
      sudo rmdir "$AGENT_DATA_DIR" 2>/dev/null || true
      sudo rm -f "$INSTALL_DIR/ai-control-agent"
      sudo rmdir "$INSTALL_DIR" 2>/dev/null || true
      echo "[systemd] removed and purged"
    else
      echo "[systemd] removed; config/SecretStore, event outbox, and installed binary preserved"
    fi
    ;;
  *)
    echo "Unknown action: $ACTION" >&2
    usage >&2
    exit 2
    ;;
esac
