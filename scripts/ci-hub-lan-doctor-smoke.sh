#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCTOR="$ROOT/scripts/ai-control-hub-lan-doctor.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
BIN="$TMP/bin"
mkdir -p "$BIN"

cat > "$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
if [[ "${FAKE_SERVICE_ACTIVE:-1}" == 1 ]]; then exit 0; fi
exit 3
EOF

cat > "$BIN/ss" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -lntH)
    if [[ "${FAKE_SS_MODE:-ok}" != missing-tcp ]]; then
      echo 'LISTEN 0 4096 0.0.0.0:8787 0.0.0.0:*'
    fi
    ;;
  -lnuH)
    if [[ "${FAKE_SS_MODE:-ok}" != missing-udp ]]; then
      echo 'UNCONN 0 0 0.0.0.0:8788 0.0.0.0:*'
    fi
    ;;
esac
EOF

cat > "$BIN/curl" <<'EOF'
#!/usr/bin/env bash
if [[ "${FAKE_HEALTH_OK:-1}" == 1 ]]; then exit 0; fi
exit 22
EOF

cat > "$BIN/firewall-cmd" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == '--state' ]]; then
  echo running
  exit 0
fi
if [[ "$1" == '--get-active-zones' ]]; then
  printf 'public\n  interfaces: eth0\n'
  exit 0
fi
permanent=0
port=''
for arg in "$@"; do
  [[ "$arg" == '--permanent' ]] && permanent=1
  case "$arg" in
    --query-port=*) port="${arg#--query-port=}" ;;
  esac
done
mode="${FAKE_FIREWALL_MODE:-ok}"
if [[ "$port" == '8787/tcp' ]]; then
  echo yes
  exit 0
fi
if [[ "$port" == '8788/udp' ]]; then
  if [[ "$mode" == runtime-missing && "$permanent" == 0 ]]; then
    echo no
    exit 1
  fi
  if [[ "$mode" == permanent-missing && "$permanent" == 1 ]]; then
    echo no
    exit 1
  fi
  echo yes
  exit 0
fi
echo no
exit 1
EOF
chmod +x "$BIN"/*

run_expect() {
  local expected="$1" label="$2"
  shift 2
  local output="$TMP/${label}.log"
  set +e
  PATH="$BIN:$PATH" "$@" bash "$DOCTOR" >"$output" 2>&1
  local rc=$?
  set -e
  if [[ "$rc" -ne "$expected" ]]; then
    echo "[$label] expected rc=$expected got rc=$rc" >&2
    cat "$output" >&2
    exit 1
  fi
  echo "[$label] rc=$rc"
  cat "$output"
}

bash -n "$DOCTOR"

run_expect 0 pass env FAKE_FIREWALL_MODE=ok

grep -Fq 'RESULT pass' "$TMP/pass.log"
grep -Fq 'UDP discovery listener present on port 8788' "$TMP/pass.log"

run_expect 2 runtime-firewall env FAKE_FIREWALL_MODE=runtime-missing
grep -Fq 'no active firewalld zone currently allows both TCP 8787 and UDP 8788' "$TMP/runtime-firewall.log"

run_expect 2 permanent-firewall env FAKE_FIREWALL_MODE=permanent-missing
grep -Fq 'reboot/reload may break discovery' "$TMP/permanent-firewall.log"

run_expect 1 missing-udp env FAKE_SS_MODE=missing-udp
grep -Fq 'UDP discovery listener missing on port 8788' "$TMP/missing-udp.log"

run_expect 1 health-fail env FAKE_HEALTH_OK=0
grep -Fq 'Hub health endpoint failed' "$TMP/health-fail.log"

echo '[hub-lan-doctor-smoke] PASSED'
