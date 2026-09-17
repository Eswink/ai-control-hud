#!/usr/bin/env bash
set -euo pipefail

MANAGER=""
CANDIDATE=""
BINARY_PATH="${AI_CONTROL_AGENT_BINARY_PATH:-/usr/local/lib/ai-control-hud/ai-control-agent}"
SYSTEMD_SERVICE="${AI_CONTROL_AGENT_SYSTEMD_SERVICE:-ai-control-hud.service}"
LAUNCHD_LABEL="${AI_CONTROL_AGENT_LAUNCHD_LABEL:-com.aicontrolhud.agent}"
LAUNCHD_PLIST="${AI_CONTROL_AGENT_LAUNCHD_PLIST:-/Library/LaunchDaemons/com.aicontrolhud.agent.plist}"
SYSTEMCTL_BIN="${AI_CONTROL_AGENT_SYSTEMCTL:-systemctl}"
LAUNCHCTL_BIN="${AI_CONTROL_AGENT_LAUNCHCTL:-launchctl}"
SUDO_BIN="${AI_CONTROL_AGENT_SUDO-sudo}"
BINARY_OWNER="${AI_CONTROL_AGENT_BINARY_OWNER:-root}"
BINARY_GROUP="${AI_CONTROL_AGENT_BINARY_GROUP:-root}"
STABLE_CHECKS="${AI_CONTROL_AGENT_STABLE_CHECKS:-4}"
STABLE_DELAY="${AI_CONTROL_AGENT_STABLE_DELAY:-0.5}"

usage() {
  cat <<'EOF'
Usage:
  ai-control-agent-unix-upgrade.sh --manager systemd|launchd --agent PATH

Transactionally replace the installed Unix Agent binary while preserving the
existing machine config, CommandCode/Hub SecretStores, service definition, and
service active/inactive state.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --manager) MANAGER="${2:-}"; shift 2 ;;
    --agent) CANDIDATE="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

run_root() {
  if [[ -n "$SUDO_BIN" ]]; then
    "$SUDO_BIN" "$@"
  else
    "$@"
  fi
}

fail() {
  echo "[agent-upgrade] $*" >&2
  exit 1
}

case "$MANAGER" in
  systemd|launchd) ;;
  *) echo "--manager must be systemd or launchd" >&2; exit 2 ;;
esac
[[ "$STABLE_CHECKS" =~ ^[1-9][0-9]*$ ]] || fail "stable check count is invalid"
[[ -n "$CANDIDATE" && -f "$CANDIDATE" ]] || {
  echo "--agent must point to a candidate ai-control-agent executable" >&2
  exit 2
}
CANDIDATE_ABS="$(cd "$(dirname "$CANDIDATE")" && pwd)/$(basename "$CANDIDATE")"
[[ -x "$CANDIDATE_ABS" ]] || {
  echo "candidate Agent binary is not executable: $CANDIDATE_ABS" >&2
  exit 2
}
[[ -f "$BINARY_PATH" ]] || fail "installed Agent binary is missing: $BINARY_PATH"
installed_abs="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"
[[ "$CANDIDATE_ABS" != "$installed_abs" ]] || fail "candidate is the installed Agent binary; use a separate new binary"

candidate_version="$("$CANDIDATE_ABS" version 2>/dev/null)" || fail "candidate failed 'version' preflight"
[[ -n "$candidate_version" && ${#candidate_version} -le 256 && "$candidate_version" != *$'\n'* && "$candidate_version" != *$'\r'* ]] || \
  fail "candidate returned an invalid version string"

service_state() {
  case "$MANAGER" in
    systemd)
      "$SYSTEMCTL_BIN" is-active "$SYSTEMD_SERVICE" 2>/dev/null || true
      ;;
    launchd)
      local output
      if output="$(run_root "$LAUNCHCTL_BIN" print "system/$LAUNCHD_LABEL" 2>/dev/null)"; then
        if grep -Eq 'state[[:space:]]*=[[:space:]]*running' <<<"$output"; then
          printf '%s\n' active
        else
          printf '%s\n' loaded-not-running
        fi
      else
        printf '%s\n' inactive
      fi
      ;;
  esac
}

start_service() {
  case "$MANAGER" in
    systemd) run_root "$SYSTEMCTL_BIN" start "$SYSTEMD_SERVICE" ;;
    launchd) run_root "$LAUNCHCTL_BIN" bootstrap system "$LAUNCHD_PLIST" ;;
  esac
}

stop_service() {
  case "$MANAGER" in
    systemd) run_root "$SYSTEMCTL_BIN" stop "$SYSTEMD_SERVICE" ;;
    launchd) run_root "$LAUNCHCTL_BIN" bootout "system/$LAUNCHD_LABEL" ;;
  esac
}

service_running_now() {
  case "$MANAGER" in
    systemd)
      "$SYSTEMCTL_BIN" is-active --quiet "$SYSTEMD_SERVICE"
      ;;
    launchd)
      local output
      output="$(run_root "$LAUNCHCTL_BIN" print "system/$LAUNCHD_LABEL" 2>/dev/null)" || return 1
      grep -Eq 'state[[:space:]]*=[[:space:]]*running' <<<"$output"
      ;;
  esac
}

wait_stable_running() {
  local count=0
  local attempts=0
  local max_attempts=$((STABLE_CHECKS * 8))
  while (( attempts < max_attempts )); do
    attempts=$((attempts + 1))
    if service_running_now; then
      count=$((count + 1))
      if (( count >= STABLE_CHECKS )); then
        return 0
      fi
    else
      count=0
    fi
    sleep "$STABLE_DELAY"
  done
  return 1
}

state="$(service_state)"
case "$state" in
  active) was_active=1 ;;
  inactive) was_active=0 ;;
  *) fail "Agent service must be active/running or inactive/unloaded before upgrade; current state=$state" ;;
esac

backup="$BINARY_PATH.upgrade.bak"
staged="$BINARY_PATH.upgrade.new"
[[ ! -e "$backup" ]] || fail "stale upgrade backup exists: $backup; refusing to overwrite the last-known-good binary"
run_root rm -f "$staged"
run_root install -o "$BINARY_OWNER" -g "$BINARY_GROUP" -m 0755 "$CANDIDATE_ABS" "$staged"
sync

cleanup_staged=1
cleanup() {
  if [[ "$cleanup_staged" -eq 1 ]]; then
    run_root rm -f "$staged" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ "$was_active" -eq 1 ]]; then
  stop_service || fail "failed to stop active Agent before upgrade"
fi

if ! run_root mv "$BINARY_PATH" "$backup"; then
  if [[ "$was_active" -eq 1 ]]; then
    start_service >/dev/null 2>&1 || true
  fi
  fail "failed to stage installed Agent binary for rollback"
fi

if ! run_root mv "$staged" "$BINARY_PATH"; then
  restore_err=0
  run_root mv "$backup" "$BINARY_PATH" || restore_err=$?
  if [[ "$was_active" -eq 1 && "$restore_err" -eq 0 ]]; then
    start_service >/dev/null 2>&1 || true
  fi
  fail "failed to commit staged Agent binary; old binary restore rc=$restore_err"
fi
cleanup_staged=0

if [[ "$was_active" -eq 1 ]]; then
  start_err=0
  start_service || start_err=$?
  if [[ "$start_err" -eq 0 ]] && wait_stable_running; then
    :
  else
    stop_service >/dev/null 2>&1 || true
    rollback_err=0
    run_root rm -f "$BINARY_PATH" || rollback_err=$?
    if [[ "$rollback_err" -eq 0 ]]; then
      run_root mv "$backup" "$BINARY_PATH" || rollback_err=$?
    fi
    restart_err=0
    if [[ "$rollback_err" -eq 0 ]]; then
      start_service || restart_err=$?
      if [[ "$restart_err" -eq 0 ]] && ! wait_stable_running; then
        restart_err=1
      fi
    fi
    if [[ "$rollback_err" -ne 0 ]]; then
      fail "new Agent failed to stabilize and binary rollback failed rc=$rollback_err"
    fi
    if [[ "$restart_err" -ne 0 ]]; then
      fail "new Agent failed to stabilize; old binary restored but old service restart failed rc=$restart_err"
    fi
    fail "new Agent failed to stabilize and was rolled back successfully"
  fi
fi

run_root rm -f "$backup"
echo "[agent-upgrade] upgraded=true version=$candidate_version manager=$MANAGER state=$state binary=$BINARY_PATH"
echo "[agent-upgrade] preserved=machine-config,commandcode-secret,hub-secret,service-definition"
