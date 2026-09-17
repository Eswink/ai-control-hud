#!/usr/bin/env bash
set -uo pipefail

http_port=8787
discovery_port=8788
health_url=""
service_name="ai-control-hub.service"
check_firewall=1

usage() {
  cat <<'EOF'
Usage: ai-control-hub-lan-doctor.sh [options]

Options:
  --http-port PORT        Hub HTTP port (default: 8787)
  --discovery-port PORT   Hub UDP discovery port (default: 8788)
  --health-url URL        Health endpoint (default: http://127.0.0.1:<http-port>/api/v1/health)
  --service NAME          systemd service name (default: ai-control-hub.service)
  --no-firewall           Skip firewalld runtime/permanent checks
  -h, --help              Show this help

Exit codes:
  0  required LAN checks passed
  1  Hub/service/listener/health failure
  2  Hub appears healthy but LAN/firewall verification is incomplete or blocked
EOF
}

valid_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && (( 1 <= 10#$1 && 10#$1 <= 65535 ))
}

while (($#)); do
  case "$1" in
    --http-port)
      [[ $# -ge 2 ]] || { echo "missing value for --http-port" >&2; exit 64; }
      http_port="$2"; shift 2 ;;
    --discovery-port)
      [[ $# -ge 2 ]] || { echo "missing value for --discovery-port" >&2; exit 64; }
      discovery_port="$2"; shift 2 ;;
    --health-url)
      [[ $# -ge 2 ]] || { echo "missing value for --health-url" >&2; exit 64; }
      health_url="$2"; shift 2 ;;
    --service)
      [[ $# -ge 2 ]] || { echo "missing value for --service" >&2; exit 64; }
      service_name="$2"; shift 2 ;;
    --no-firewall)
      check_firewall=0; shift ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 64 ;;
  esac
done

valid_port "$http_port" || { echo "invalid HTTP port: $http_port" >&2; exit 64; }
valid_port "$discovery_port" || { echo "invalid discovery port: $discovery_port" >&2; exit 64; }
if [[ -z "$health_url" ]]; then
  health_url="http://127.0.0.1:${http_port}/api/v1/health"
fi

failures=0
warnings=0

pass() { printf '[hub-lan-doctor] PASS %s\n' "$*"; }
warn() { printf '[hub-lan-doctor] WARN %s\n' "$*"; warnings=$((warnings + 1)); }
fail() { printf '[hub-lan-doctor] FAIL %s\n' "$*"; failures=$((failures + 1)); }
info() { printf '[hub-lan-doctor] INFO %s\n' "$*"; }

if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active --quiet "$service_name" 2>/dev/null; then
    pass "systemd service active: $service_name"
  else
    fail "systemd service is not active: $service_name"
  fi
else
  warn "systemctl unavailable; service state not verified"
fi

has_listener() {
  local mode="$1" port="$2"
  ss "$mode" 2>/dev/null | awk -v suffix=":${port}" '
    $4 ~ suffix "$" { found = 1 }
    END { exit(found ? 0 : 1) }
  '
}

if command -v ss >/dev/null 2>&1; then
  if has_listener -lntH "$http_port"; then
    pass "TCP listener present on port $http_port"
  else
    fail "TCP listener missing on port $http_port"
  fi
  if has_listener -lnuH "$discovery_port"; then
    pass "UDP discovery listener present on port $discovery_port"
  else
    fail "UDP discovery listener missing on port $discovery_port"
  fi
else
  warn "ss unavailable; TCP/UDP listeners not verified"
fi

if command -v curl >/dev/null 2>&1; then
  if curl -fsS --max-time 3 "$health_url" >/dev/null 2>&1; then
    pass "Hub health endpoint reachable: $health_url"
  else
    fail "Hub health endpoint failed: $health_url"
  fi
else
  warn "curl unavailable; Hub health endpoint not verified"
fi

query_zone_port() {
  local zone="$1" port_proto="$2" permanent="$3"
  if [[ "$permanent" == 1 ]]; then
    firewall-cmd --permanent --zone="$zone" --query-port="$port_proto" 2>/dev/null | grep -qx yes
  else
    firewall-cmd --zone="$zone" --query-port="$port_proto" 2>/dev/null | grep -qx yes
  fi
}

if (( check_firewall )); then
  if command -v firewall-cmd >/dev/null 2>&1 && [[ "$(firewall-cmd --state 2>/dev/null || true)" == running ]]; then
    mapfile -t zones < <(firewall-cmd --get-active-zones 2>/dev/null | awk 'NF && $0 !~ /^[[:space:]]/ {print $1}')
    if ((${#zones[@]} == 0)); then
      warn "firewalld is running but no active zones were found"
    else
      runtime_ready=0
      permanent_ready=0
      for zone in "${zones[@]}"; do
        runtime_tcp=0; runtime_udp=0; permanent_tcp=0; permanent_udp=0
        query_zone_port "$zone" "${http_port}/tcp" 0 && runtime_tcp=1
        query_zone_port "$zone" "${discovery_port}/udp" 0 && runtime_udp=1
        query_zone_port "$zone" "${http_port}/tcp" 1 && permanent_tcp=1
        query_zone_port "$zone" "${discovery_port}/udp" 1 && permanent_udp=1

        info "firewalld zone=$zone runtime tcp:${http_port}=$runtime_tcp udp:${discovery_port}=$runtime_udp permanent tcp:${http_port}=$permanent_tcp udp:${discovery_port}=$permanent_udp"
        (( runtime_tcp && runtime_udp )) && runtime_ready=1
        (( permanent_tcp && permanent_udp )) && permanent_ready=1
      done

      if (( runtime_ready )); then
        pass "at least one active firewalld zone allows TCP $http_port and UDP $discovery_port at runtime"
      else
        warn "no active firewalld zone currently allows both TCP $http_port and UDP $discovery_port"
      fi
      if (( permanent_ready )); then
        pass "at least one active firewalld zone permanently allows TCP $http_port and UDP $discovery_port"
      else
        warn "no active firewalld zone permanently allows both TCP $http_port and UDP $discovery_port; reboot/reload may break discovery"
      fi
    fi
  elif command -v firewall-cmd >/dev/null 2>&1; then
    info "firewalld is installed but not running"
  else
    info "firewall-cmd unavailable; no firewalld policy checked"
  fi
else
  info "firewall checks skipped by --no-firewall"
fi

if (( failures > 0 )); then
  printf '[hub-lan-doctor] RESULT fail failures=%d warnings=%d\n' "$failures" "$warnings"
  exit 1
fi
if (( warnings > 0 )); then
  printf '[hub-lan-doctor] RESULT warning failures=0 warnings=%d\n' "$warnings"
  exit 2
fi
printf '[hub-lan-doctor] RESULT pass failures=0 warnings=0\n'
