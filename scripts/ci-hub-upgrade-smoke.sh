#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="$ROOT/scripts/ai-control-hub-systemd.sh"
TMP="$(mktemp -d)"
trap 'sudo rm -rf "$TMP"' EXIT

INSTALL="$TMP/install"
CONFIG="$TMP/config"
DATA="$TMP/data"
SYSTEMD="$TMP/systemd-runtime"
UNIT="$TMP/ai-control-hub.service"
ENV_FILE="$CONFIG/hub.env"
FAKE_SYSTEMCTL="$TMP/fake-systemctl"
SYSTEMCTL_LOG="$TMP/systemctl.log"
STATE_FILE="$TMP/service-active"
FAIL_START_FILE="$TMP/fail-next-start"
mkdir -p "$INSTALL" "$CONFIG" "$DATA" "$SYSTEMD"

cat > "$FAKE_SYSTEMCTL" <<EOF
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "\$*" >> "$SYSTEMCTL_LOG"
case "\${1:-}" in
  is-active)
    [[ -f "$STATE_FILE" ]]
    ;;
  stop)
    rm -f "$STATE_FILE"
    ;;
  start|restart)
    if [[ -f "$FAIL_START_FILE" ]]; then
      rm -f "$FAIL_START_FILE"
      exit 1
    fi
    touch "$STATE_FILE"
    ;;
  *)
    exit 0
    ;;
esac
EOF
chmod +x "$FAKE_SYSTEMCTL"

write_binary() {
  local path="$1" version="$2"
  cat > "$path" <<EOF
#!/usr/bin/env bash
if [[ "\${1:-}" == version ]]; then
  echo '$version'
  exit 0
fi
echo 'unexpected synthetic binary invocation' >&2
exit 2
EOF
  chmod +x "$path"
}

write_supported_unit() {
  cat > "$UNIT" <<EOF
[Service]
Environment=HUD_HUB_ID=dorm-hub
Environment=HUD_HUB_DISCOVERY_ENABLED=1
Environment=HUD_HUB_DISCOVERY_PORT=8788
Environment=HUD_HUB_HTTP_PORT=8787
ExecStart=$INSTALL/ai-control-hub serve --host 0.0.0.0 --port 8787
EOF
}

cat > "$ENV_FILE" <<'EOF'
HUD_HUB_DB=/var/lib/ai-control-hud/hub.sqlite3
HUD_HUB_AGENT_ID=desktop-main
HUD_HUB_AGENT_TOKEN=THIS_IS_A_SYNTHETIC_TOKEN_VALUE_0123456789
HUD_HUB_STALE_AFTER_SECONDS=45
EOF
printf 'persistent sqlite marker\n' > "$DATA/hub.sqlite3"
write_supported_unit
write_binary "$INSTALL/ai-control-hub" '1.0.0'
printf 'old doctor\n' > "$INSTALL/ai-control-hub-lan-doctor.sh"
chmod +x "$INSTALL/ai-control-hub-lan-doctor.sh"

env_hash="$(sha256sum "$ENV_FILE" | awk '{print $1}')"
unit_hash="$(sha256sum "$UNIT" | awk '{print $1}')"
data_hash="$(sha256sum "$DATA/hub.sqlite3" | awk '{print $1}')"

run_upgrade() {
  env \
    AI_CONTROL_HUB_INSTALL_DIR="$INSTALL" \
    AI_CONTROL_HUB_CONFIG_DIR="$CONFIG" \
    AI_CONTROL_HUB_DATA_DIR="$DATA" \
    AI_CONTROL_HUB_ENV_PATH="$ENV_FILE" \
    AI_CONTROL_HUB_UNIT_PATH="$UNIT" \
    AI_CONTROL_HUB_SYSTEMD_RUNTIME_DIR="$SYSTEMD" \
    AI_CONTROL_HUB_SYSTEMCTL_BIN="$FAKE_SYSTEMCTL" \
    bash "$INSTALLER" upgrade --binary "$1"
}

assert_preserved() {
  test "$(sha256sum "$ENV_FILE" | awk '{print $1}')" = "$env_hash"
  test "$(sha256sum "$UNIT" | awk '{print $1}')" = "$unit_hash"
  test "$(sha256sum "$DATA/hub.sqlite3" | awk '{print $1}')" = "$data_hash"
}

# Active service: stop -> atomic replacement -> start, no token file required.
touch "$STATE_FILE"
: > "$SYSTEMCTL_LOG"
write_binary "$TMP/hub-2" '2.0.0'
run_upgrade "$TMP/hub-2"
test "$(sudo "$INSTALL/ai-control-hub" version)" = '2.0.0'
test -f "$STATE_FILE"
grep -Fxq 'is-active --quiet ai-control-hub.service' "$SYSTEMCTL_LOG"
grep -Fxq 'stop ai-control-hub.service' "$SYSTEMCTL_LOG"
grep -Fxq 'start ai-control-hub.service' "$SYSTEMCTL_LOG"
assert_preserved

# Stopped service stays stopped; upgrade must not surprise-start it.
rm -f "$STATE_FILE"
: > "$SYSTEMCTL_LOG"
write_binary "$TMP/hub-3" '3.0.0'
run_upgrade "$TMP/hub-3"
test "$(sudo "$INSTALL/ai-control-hub" version)" = '3.0.0'
test ! -f "$STATE_FILE"
grep -Fxq 'is-active --quiet ai-control-hub.service' "$SYSTEMCTL_LOG"
if grep -Eq '^(stop|start|restart) ai-control-hub.service$' "$SYSTEMCTL_LOG"; then
  echo 'stopped service lifecycle changed during upgrade' >&2
  cat "$SYSTEMCTL_LOG" >&2
  exit 1
fi
assert_preserved

# Failed start rolls the binary back and restores the prior active state.
touch "$STATE_FILE" "$FAIL_START_FILE"
: > "$SYSTEMCTL_LOG"
write_binary "$TMP/hub-4" '4.0.0'
set +e
run_upgrade "$TMP/hub-4" >"$TMP/rollback.log" 2>&1
rc=$?
set -e
[[ "$rc" -eq 1 ]] || { cat "$TMP/rollback.log" >&2; exit 1; }
grep -Fq 'restoring previous binary' "$TMP/rollback.log"
test "$(sudo "$INSTALL/ai-control-hub" version)" = '3.0.0'
test -f "$STATE_FILE"
assert_preserved

# Unknown/legacy service layouts must use explicit migration, not secretless upgrade.
cat > "$UNIT" <<'EOF'
[Service]
ExecStart=/usr/bin/python -m legacy.hub
EOF
legacy_hash="$(sha256sum "$UNIT" | awk '{print $1}')"
write_binary "$TMP/hub-5" '5.0.0'
set +e
run_upgrade "$TMP/hub-5" >"$TMP/legacy.log" 2>&1
rc=$?
set -e
[[ "$rc" -eq 1 ]] || { cat "$TMP/legacy.log" >&2; exit 1; }
grep -Fq 'not the supported Go Hub layout' "$TMP/legacy.log"
test "$(sudo "$INSTALL/ai-control-hub" version)" = '3.0.0'
test "$(sha256sum "$UNIT" | awk '{print $1}')" = "$legacy_hash"
test "$(sha256sum "$ENV_FILE" | awk '{print $1}')" = "$env_hash"
test "$(sha256sum "$DATA/hub.sqlite3" | awk '{print $1}')" = "$data_hash"

echo '[hub-upgrade-smoke] PASSED'
