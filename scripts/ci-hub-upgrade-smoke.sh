#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPGRADE="$ROOT/scripts/ai-control-hub-upgrade.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/install" "$tmp/state"

installed="$tmp/install/ai-control-hub"
state_file="$tmp/state/service-state"
env_file="$tmp/state/hub.env"
db_file="$tmp/state/hub.sqlite3"
unit_file="$tmp/state/ai-control-hub.service"
fake_systemctl="$tmp/bin/systemctl"
old_candidate="$tmp/old-hub"
good_one="$tmp/good-one"
good_two="$tmp/good-two"
bad_candidate="$tmp/bad-service"

cat >"$fake_systemctl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
state_file="${FAKE_HUB_STATE_FILE:?}"
target="${FAKE_HUB_BINARY:?}"
command="${1:-}"
shift || true
case "$command" in
  is-active)
    quiet=0
    if [[ "${1:-}" == "--quiet" ]]; then
      quiet=1
      shift
    fi
    state="$(cat "$state_file")"
    if [[ "$quiet" -eq 0 ]]; then
      printf '%s\n' "$state"
    fi
    [[ "$state" == "active" ]]
    ;;
  stop)
    printf '%s\n' inactive >"$state_file"
    ;;
  start)
    if grep -Fq 'BROKEN_SERVICE=1' "$target"; then
      printf '%s\n' failed >"$state_file"
      exit 1
    fi
    printf '%s\n' active >"$state_file"
    ;;
  *)
    echo "unexpected fake systemctl command: $command $*" >&2
    exit 97
    ;;
esac
SH
chmod +x "$fake_systemctl"

write_candidate() {
  local path="$1"
  local version="$2"
  local broken="$3"
  cat >"$path" <<EOF
#!/usr/bin/env bash
set -euo pipefail
BROKEN_SERVICE=$broken
if [[ "\${1:-}" == "version" ]]; then
  echo "$version"
  exit 0
fi
exit 0
EOF
  chmod +x "$path"
}

write_candidate "$old_candidate" "1.0.0-old" 0
write_candidate "$good_one" "1.1.0-good" 0
write_candidate "$good_two" "1.2.0-good" 0
write_candidate "$bad_candidate" "9.9.9-broken" 1
cp "$old_candidate" "$installed"
chmod +x "$installed"
printf '%s\n' active >"$state_file"
printf '%s\n' 'HUD_HUB_AGENT_TOKEN=SECRET_CANARY' >"$env_file"
printf '%s\n' 'SQLITE_CANARY' >"$db_file"
printf '%s\n' 'UNIT_CANARY' >"$unit_file"

hash_sentinels() {
  sha256sum "$env_file" "$db_file" "$unit_file" | awk '{print $1}' | tr '\n' ':'
}

assert_preserved() {
  local expected="$1"
  local actual
  actual="$(hash_sentinels)"
  [[ "$actual" == "$expected" ]] || {
    echo "Hub env/db/unit sentinel changed during binary upgrade" >&2
    exit 1
  }
}

assert_version() {
  local expected="$1"
  local actual
  actual="$("$installed" version)"
  [[ "$actual" == "$expected" ]] || {
    echo "installed Hub version=$actual want=$expected" >&2
    exit 1
  }
}

assert_scratch_clean() {
  [[ ! -e "$installed.upgrade.new" ]] || { echo "staged upgrade remains" >&2; exit 1; }
  [[ ! -e "$installed.upgrade.bak" ]] || { echo "upgrade backup remains" >&2; exit 1; }
}

run_upgrade() {
  env \
    AI_CONTROL_HUB_SERVICE_NAME=ai-control-hub.service \
    AI_CONTROL_HUB_BINARY_PATH="$installed" \
    AI_CONTROL_HUB_SYSTEMCTL="$fake_systemctl" \
    AI_CONTROL_HUB_SUDO= \
    AI_CONTROL_HUB_BINARY_OWNER="$(id -un)" \
    AI_CONTROL_HUB_BINARY_GROUP="$(id -gn)" \
    FAKE_HUB_STATE_FILE="$state_file" \
    FAKE_HUB_BINARY="$installed" \
    bash "$UPGRADE" --binary "$1"
}

bash -n "$UPGRADE"
baseline_hash="$(hash_sentinels)"

echo "[hub-upgrade-smoke] active successful upgrade"
run_upgrade "$good_one"
assert_version "1.1.0-good"
[[ "$(cat "$state_file")" == "active" ]]
assert_preserved "$baseline_hash"
assert_scratch_clean
[[ "$(stat -c '%a' "$installed")" == "755" ]]

echo "[hub-upgrade-smoke] active failed candidate rolls back"
if run_upgrade "$bad_candidate"; then
  echo "broken Hub service candidate unexpectedly succeeded" >&2
  exit 1
fi
assert_version "1.1.0-good"
[[ "$(cat "$state_file")" == "active" ]]
assert_preserved "$baseline_hash"
assert_scratch_clean

echo "[hub-upgrade-smoke] inactive upgrade preserves inactive state"
printf '%s\n' inactive >"$state_file"
run_upgrade "$good_two"
assert_version "1.2.0-good"
[[ "$(cat "$state_file")" == "inactive" ]]
assert_preserved "$baseline_hash"
assert_scratch_clean

echo "[hub-upgrade-smoke] stale rollback backup is protected"
cp "$old_candidate" "$installed.upgrade.bak"
if run_upgrade "$good_one"; then
  echo "upgrade unexpectedly overwrote stale rollback backup" >&2
  exit 1
fi
assert_version "1.2.0-good"
[[ -f "$installed.upgrade.bak" ]]
assert_preserved "$baseline_hash"
rm -f "$installed.upgrade.bak"

echo "[hub-upgrade-smoke] ok"