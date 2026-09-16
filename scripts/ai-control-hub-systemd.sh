#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="ai-control-hub.service"
SERVICE_USER="ai-control-hub"
INSTALL_DIR="/usr/local/lib/ai-control-hub"
BINARY_PATH="$INSTALL_DIR/ai-control-hub"
DATA_DIR="/var/lib/ai-control-hud"
CONFIG_DIR="/etc/ai-control-hud"
ENV_PATH="$CONFIG_DIR/hub.env"
UNIT_PATH="/etc/systemd/system/$SERVICE_NAME"

LISTEN="127.0.0.1:8787"
LISTEN_PROVIDED=0
LAN_AUTO=0
DISCOVERY_PORT=8788
HUB_ID="central-hub"
AGENT_ID="desktop-main"
BINARY=""
TOKEN_FILE=""
PURGE=0
PURGE_DATA=0

usage() {
  cat <<'EOF'
Usage:
  ai-control-hub-systemd.sh install --binary PATH --token-file PATH [--listen HOST:PORT | --lan-auto] [--discovery-port PORT] [--hub-id ID] [--agent-id ID]
  ai-control-hub-systemd.sh rotate-token --token-file PATH
  ai-control-hub-systemd.sh render-unit [--listen HOST:PORT | --lan-auto] [--discovery-port PORT] [--hub-id ID]
  ai-control-hub-systemd.sh start|stop|restart|status
  ai-control-hub-systemd.sh remove [--purge] [--purge-data]

Go Hub deployment:
  * CentOS does not need Python, pip, venv, or a Go toolchain
  * install consumes the prebuilt Linux ai-control-hub binary from the validation/release bundle

Security defaults:
  * listen defaults to 127.0.0.1:8787
  * --lan-auto explicitly binds 0.0.0.0:8787 and enables UDP LAN discovery on port 8788
  * LAN discovery advertises only service metadata; it never sends the bearer token
  * the token file is imported into /etc/ai-control-hud/hub.env (0600, root-only)
  * SQLite state lives in /var/lib/ai-control-hud and is preserved on normal removal
  * token rotation never accepts a bearer token on the command line
  * --purge-data requires --purge and permanently deletes the Hub database
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
    --binary) BINARY="$2"; shift 2 ;;
    --token-file) TOKEN_FILE="$2"; shift 2 ;;
    --listen) LISTEN="$2"; LISTEN_PROVIDED=1; shift 2 ;;
    --lan-auto) LAN_AUTO=1; shift ;;
    --discovery-port) DISCOVERY_PORT="$2"; shift 2 ;;
    --hub-id) HUB_ID="$2"; shift 2 ;;
    --agent-id) AGENT_ID="$2"; shift 2 ;;
    --purge) PURGE=1; shift ;;
    --purge-data) PURGE_DATA=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ "$LAN_AUTO" -eq 1 && "$LISTEN_PROVIDED" -eq 1 ]]; then
  echo "--lan-auto and --listen are mutually exclusive" >&2
  exit 2
fi
if [[ "$LAN_AUTO" -eq 1 ]]; then
  LISTEN="0.0.0.0:8787"
fi

validate_listen() {
  local value="${1:-$LISTEN}"
  if [[ ! "$value" =~ ^[A-Za-z0-9._-]+:([0-9]+)$ ]]; then
    echo "--listen must use HOST:PORT with an IPv4 address or hostname" >&2
    exit 2
  fi
  local port="${BASH_REMATCH[1]}"
  if (( port < 1 || port > 65535 )); then
    echo "--listen port must be within 1..65535" >&2
    exit 2
  fi
}

validate_port() {
  local value="$1"
  local label="$2"
  if [[ ! "$value" =~ ^[0-9]+$ ]] || (( value < 1 || value > 65535 )); then
    echo "$label must be within 1..65535" >&2
    exit 2
  fi
}

validate_identifier() {
  local value="$1"
  local label="$2"
  [[ "$value" =~ ^[A-Za-z0-9._:-]{1,128}$ ]] || {
    echo "$label contains unsupported characters" >&2
    exit 2
  }
}

read_token_file() {
  local path="$1"
  [[ -n "$path" && -f "$path" ]] || {
    echo "--token-file must point to a readable token file" >&2
    exit 2
  }
  local value
  value="$(cat "$path")"
  [[ ! "$value" =~ $'\n' && ! "$value" =~ $'\r' ]] || {
    echo "hub token must be a single line" >&2
    unset value
    exit 2
  }
  (( ${#value} >= 32 && ${#value} <= 512 )) || {
    echo "hub token length must be within 32..512 characters" >&2
    unset value
    exit 2
  }
  [[ "$value" =~ ^[A-Za-z0-9._~-]+$ ]] || {
    echo "hub token must use URL-safe printable characters" >&2
    unset value
    exit 2
  }
  printf '%s' "$value"
  unset value
}

listen_host() {
  printf '%s\n' "${LISTEN%:*}"
}

listen_port() {
  printf '%s\n' "${LISTEN##*:}"
}

print_lan_urls() {
  local port="$1"
  local found=0
  if command -v ip >/dev/null 2>&1; then
    while read -r address; do
      [[ -n "$address" && "$address" != 127.* ]] || continue
      echo "[hub-systemd] LAN candidate=http://$address:$port"
      found=1
    done < <(ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | sort -u)
  elif command -v hostname >/dev/null 2>&1; then
    for address in $(hostname -I 2>/dev/null || true); do
      [[ "$address" == *.* && "$address" != 127.* ]] || continue
      echo "[hub-systemd] LAN candidate=http://$address:$port"
      found=1
    done
  fi
  if [[ "$found" -eq 0 ]]; then
    echo "[hub-systemd] LAN address unavailable; clients can still discover the Hub after networking is up"
  fi
}

render_unit() {
  validate_listen "$LISTEN"
  validate_port "$DISCOVERY_PORT" "--discovery-port"
  validate_identifier "$HUB_ID" "hub id"
  local host port discovery_enabled
  host="$(listen_host)"
  port="$(listen_port)"
  discovery_enabled=0
  if [[ "$LAN_AUTO" -eq 1 ]]; then
    discovery_enabled=1
  fi
  cat <<EOF
[Unit]
Description=AI Control HUD Central Hub (Go)
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$DATA_DIR
EnvironmentFile=$ENV_PATH
Environment=HUD_HUB_ID=$HUB_ID
Environment=HUD_HUB_DISCOVERY_ENABLED=$discovery_enabled
Environment=HUD_HUB_DISCOVERY_PORT=$DISCOVERY_PORT
Environment=HUD_HUB_HTTP_SCHEME=http
Environment=HUD_HUB_HTTP_PORT=$port
ExecStart=$BINARY_PATH serve --host $host --port $port
Restart=on-failure
RestartSec=5s
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
LockPersonality=true
RestrictSUIDSGID=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF
}

write_env_temp() {
  local path="$1"
  local token="$2"
  local agent_id="$3"
  local database="$4"
  local stale_after="$5"
  umask 077
  cat >"$path" <<EOF
HUD_HUB_DB=$database
HUD_HUB_AGENT_ID=$agent_id
HUD_HUB_AGENT_TOKEN=$token
HUD_HUB_STALE_AFTER_SECONDS=$stale_after
EOF
  chmod 0600 "$path"
}

require_systemd() {
  command -v systemctl >/dev/null 2>&1 || { echo "systemctl is unavailable" >&2; exit 1; }
  [[ -d /run/systemd/system ]] || { echo "systemd is not the active service manager" >&2; exit 1; }
}

read_env_value() {
  local key="$1"
  sudo sed -n "s/^${key}=//p" "$ENV_PATH" | tail -n 1
}

case "$ACTION" in
  render-unit)
    render_unit
    ;;
  install)
    require_systemd
    validate_listen "$LISTEN"
    validate_port "$DISCOVERY_PORT" "--discovery-port"
    validate_identifier "$HUB_ID" "hub id"
    validate_identifier "$AGENT_ID" "agent id"
    [[ -n "$BINARY" && -f "$BINARY" ]] || { echo "--binary must point to the Linux ai-control-hub executable" >&2; exit 2; }
    binary_abs="$(cd "$(dirname "$BINARY")" && pwd)/$(basename "$BINARY")"
    [[ -x "$binary_abs" ]] || chmod u+x "$binary_abs"
    "$binary_abs" version >/dev/null 2>&1 || {
      echo "Hub binary could not run on this host; verify Linux architecture/artifact" >&2
      exit 1
    }
    token="$(read_token_file "$TOKEN_FILE")"

    if ! id "$SERVICE_USER" >/dev/null 2>&1; then
      sudo useradd --system --home-dir "$DATA_DIR" --shell /sbin/nologin "$SERVICE_USER"
    fi
    sudo install -d -o root -g root -m 0755 "$INSTALL_DIR"
    sudo install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 "$DATA_DIR"
    sudo install -d -o root -g root -m 0755 "$CONFIG_DIR"

    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sudo install -o root -g root -m 0755 "$binary_abs" "$BINARY_PATH"

    env_tmp="$(mktemp)"
    unit_tmp="$(mktemp)"
    trap 'rm -f "$env_tmp" "$unit_tmp"; unset token' EXIT
    write_env_temp "$env_tmp" "$token" "$AGENT_ID" "$DATA_DIR/hub.sqlite3" "45"
    render_unit >"$unit_tmp"

    sudo install -o root -g root -m 0600 "$env_tmp" "$ENV_PATH"
    sudo install -o root -g root -m 0644 "$unit_tmp" "$UNIT_PATH"
    if command -v systemd-analyze >/dev/null 2>&1; then
      sudo systemd-analyze verify "$UNIT_PATH" >/dev/null
    fi
    sudo systemctl daemon-reload
    sudo systemctl enable "$SERVICE_NAME"
    unset token
    echo "[hub-systemd] installed Go Hub service=$SERVICE_NAME listen=$LISTEN data=$DATA_DIR binary=$BINARY_PATH"
    if [[ "$LAN_AUTO" -eq 1 ]]; then
      echo "[hub-systemd] LAN auto-discovery enabled udp=$DISCOVERY_PORT hub=$HUB_ID"
      print_lan_urls "$(listen_port)"
    fi
    echo "[hub-systemd] service is enabled but not started; run '$0 start' after network/firewall validation"
    echo "[hub-systemd] plaintext token file was not modified; delete it after end-to-end validation"
    ;;
  rotate-token)
    require_systemd
    [[ -f "$ENV_PATH" ]] || { echo "Hub environment file is missing: $ENV_PATH" >&2; exit 1; }
    token="$(read_token_file "$TOKEN_FILE")"
    database="$(read_env_value HUD_HUB_DB)"
    agent_id="$(read_env_value HUD_HUB_AGENT_ID)"
    stale_after="$(read_env_value HUD_HUB_STALE_AFTER_SECONDS)"
    [[ -n "$database" && -n "$agent_id" && -n "$stale_after" ]] || {
      unset token
      echo "Hub environment file is incomplete" >&2
      exit 1
    }
    validate_identifier "$agent_id" "agent id"
    [[ "$stale_after" =~ ^[0-9]+$ ]] || { unset token; echo "Hub stale threshold is invalid" >&2; exit 1; }

    env_tmp="$(mktemp)"
    trap 'rm -f "$env_tmp"; unset token' EXIT
    write_env_temp "$env_tmp" "$token" "$agent_id" "$database" "$stale_after"
    sudo install -o root -g root -m 0600 "$env_tmp" "$ENV_PATH.new"
    sudo mv -f "$ENV_PATH.new" "$ENV_PATH"
    unset token
    if sudo systemctl is-active --quiet "$SERVICE_NAME"; then
      sudo systemctl restart "$SERVICE_NAME"
      echo "[hub-systemd] token rotated and active Hub restarted"
    else
      echo "[hub-systemd] token rotated; Hub was not active and remains stopped"
    fi
    echo "[hub-systemd] plaintext token file was not modified; delete it after end-to-end validation"
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
    if [[ "$PURGE_DATA" -eq 1 && "$PURGE" -ne 1 ]]; then
      echo "--purge-data requires --purge" >&2
      exit 2
    fi
    sudo systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    sudo systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    sudo rm -f "$UNIT_PATH"
    sudo systemctl daemon-reload
    sudo systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true
    if [[ "$PURGE" -eq 1 ]]; then
      sudo rm -f "$ENV_PATH"
      sudo rm -rf "$INSTALL_DIR"
      if [[ "$PURGE_DATA" -eq 1 ]]; then
        sudo rm -rf "$DATA_DIR"
        sudo userdel "$SERVICE_USER" 2>/dev/null || true
        echo "[hub-systemd] removed app/config and permanently purged Hub data"
      else
        echo "[hub-systemd] removed app/config; Hub data preserved at $DATA_DIR"
      fi
    else
      echo "[hub-systemd] service removed; app/config/data preserved"
    fi
    ;;
  *)
    echo "Unknown action: $ACTION" >&2
    usage >&2
    exit 2
    ;;
esac
