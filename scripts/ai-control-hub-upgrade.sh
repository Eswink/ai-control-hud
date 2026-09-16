#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${AI_CONTROL_HUB_SERVICE_NAME:-ai-control-hub.service}"
BINARY_PATH="${AI_CONTROL_HUB_BINARY_PATH:-/usr/local/lib/ai-control-hub/ai-control-hub}"
SYSTEMCTL_BIN="${AI_CONTROL_HUB_SYSTEMCTL:-systemctl}"
SUDO_BIN="${AI_CONTROL_HUB_SUDO:-sudo}"
CANDIDATE=""

usage() {
  cat <<'EOF'
Usage:
  ai-control-hub-upgrade.sh --binary PATH

Transactionally replace the installed Go Hub binary while preserving:
  * /etc/ai-control-hud/hub.env and the Hub bearer token
  * /var/lib/ai-control-hud SQLite state
  * the existing systemd unit and enabled state
  * firewall configuration

The candidate must pass `ai-control-hub version` before service downtime.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) CANDIDATE="${2:-}"; shift 2 ;;
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
  echo "[hub-upgrade] $*" >&2
  exit 1
}

[[ -n "$CANDIDATE" && -f "$CANDIDATE" ]] || {
  echo "--binary must point to a candidate ai-control-hub executable" >&2
  exit 2
}
CANDIDATE_ABS="$(cd "$(dirname "$CANDIDATE")" && pwd)/$(basename "$CANDIDATE")"
[[ -x "$CANDIDATE_ABS" ]] || {
  echo "candidate Hub binary is not executable: $CANDIDATE_ABS" >&2
  exit 2
}
[[ -f "$BINARY_PATH" ]] || fail "installed Hub binary is missing: $BINARY_PATH"

installed_abs="$(cd "$(dirname "$BINARY_PATH")" && pwd)/$(basename "$BINARY_PATH")"
if [[ "$CANDIDATE_ABS" == "$installed_abs" ]]; then
  fail "candidate is the installed Hub binary; run upgrade with a separate new binary"
fi

candidate_version="$("$CANDIDATE_ABS" version 2>/dev/null)" || fail "candidate failed 'version' preflight"
[[ -n "$candidate_version" && ${#candidate_version} -le 256 && "$candidate_version" != *$'\n'* && "$candidate_version" != *$'\r'* ]] || \
  fail "candidate returned an invalid version string"

backup="$BINARY_PATH.upgrade.bak"
staged="$BINARY_PATH.upgrade.new"
if [[ -e "$backup" ]]; then
  fail "stale upgrade backup exists: $backup; refusing to overwrite the last-known-good binary"
fi

service_state="$($SYSTEMCTL_BIN is-active "$SERVICE_NAME" 2>/dev/null || true)"
case "$service_state" in
  active) was_active=1 ;;
  inactive) was_active=0 ;;
  *) fail "Hub service must be active or inactive before upgrade; current state=${service_state:-unknown}" ;;
esac

run_root rm -f "$staged"
run_root install -o root -g root -m 0755 "$CANDIDATE_ABS" "$staged"
sync

cleanup_staged=1
cleanup() {
  if [[ "$cleanup_staged" -eq 1 ]]; then
    run_root rm -f "$staged" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ "$was_active" -eq 1 ]]; then
  run_root "$SYSTEMCTL_BIN" stop "$SERVICE_NAME" || fail "failed to stop active Hub before upgrade"
fi

if ! run_root mv "$BINARY_PATH" "$backup"; then
  if [[ "$was_active" -eq 1 ]]; then
    run_root "$SYSTEMCTL_BIN" start "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
  fail "failed to stage installed Hub binary for rollback"
fi

if ! run_root mv "$staged" "$BINARY_PATH"; then
  restore_err=0
  run_root mv "$backup" "$BINARY_PATH" || restore_err=$?
  if [[ "$was_active" -eq 1 && "$restore_err" -eq 0 ]]; then
    run_root "$SYSTEMCTL_BIN" start "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
  fail "failed to commit staged Hub binary; old binary restore rc=$restore_err"
fi
cleanup_staged=0

if [[ "$was_active" -eq 1 ]]; then
  start_err=0
  run_root "$SYSTEMCTL_BIN" start "$SERVICE_NAME" || start_err=$?
  if [[ "$start_err" -eq 0 ]] && "$SYSTEMCTL_BIN" is-active --quiet "$SERVICE_NAME"; then
    :
  else
    run_root "$SYSTEMCTL_BIN" stop "$SERVICE_NAME" >/dev/null 2>&1 || true
    rollback_err=0
    run_root rm -f "$BINARY_PATH" || rollback_err=$?
    if [[ "$rollback_err" -eq 0 ]]; then
      run_root mv "$backup" "$BINARY_PATH" || rollback_err=$?
    fi
    restart_err=0
    if [[ "$rollback_err" -eq 0 ]]; then
      run_root "$SYSTEMCTL_BIN" start "$SERVICE_NAME" || restart_err=$?
      if [[ "$restart_err" -eq 0 ]] && ! "$SYSTEMCTL_BIN" is-active --quiet "$SERVICE_NAME"; then
        restart_err=1
      fi
    fi
    if [[ "$rollback_err" -ne 0 ]]; then
      fail "new Hub failed to start and binary rollback failed rc=$rollback_err"
    fi
    if [[ "$restart_err" -ne 0 ]]; then
      fail "new Hub failed to start; old binary restored but old service restart failed rc=$restart_err"
    fi
    fail "new Hub failed to start and was rolled back successfully"
  fi
fi

run_root rm -f "$backup"
echo "[hub-upgrade] upgraded=true version=$candidate_version state=$service_state binary=$BINARY_PATH"
echo "[hub-upgrade] preserved=hub.env,sqlite,systemd-unit,firewall"
