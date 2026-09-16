#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="$ROOT/scripts/ai-control-hub-systemd.sh"
LAN_DOCTOR_SMOKE="$ROOT/scripts/ci-hub-lan-doctor-smoke.sh"

bash -n "$INSTALLER"
bash -n "$LAN_DOCTOR_SMOKE"

unit_default="$(bash "$INSTALLER" render-unit)"
grep -Fq 'ExecStart=/usr/local/lib/ai-control-hub/ai-control-hub serve --host 127.0.0.1 --port 8787' <<<"$unit_default"
grep -Fq 'Environment=HUD_HUB_DISCOVERY_ENABLED=0' <<<"$unit_default"
grep -Fq 'User=ai-control-hub' <<<"$unit_default"
grep -Fq 'EnvironmentFile=/etc/ai-control-hud/hub.env' <<<"$unit_default"
grep -Fq 'NoNewPrivileges=true' <<<"$unit_default"
grep -Fq 'ProtectSystem=strict' <<<"$unit_default"
grep -Fq 'ProtectHome=true' <<<"$unit_default"
grep -Fq 'CapabilityBoundingSet=' <<<"$unit_default"
grep -Fq 'ReadWritePaths=/var/lib/ai-control-hud' <<<"$unit_default"

if grep -Eiq 'python|uvicorn|venv' <<<"$unit_default"; then
  echo "Go Hub unit unexpectedly depends on Python/uvicorn/venv" >&2
  exit 1
fi
if grep -Fq 'HUD_HUB_AGENT_TOKEN=' <<<"$unit_default"; then
  echo "rendered unit must not contain the bearer token" >&2
  exit 1
fi

unit_overlay="$(bash "$INSTALLER" render-unit --listen 100.64.0.10:9443)"
grep -Fq 'ExecStart=/usr/local/lib/ai-control-hub/ai-control-hub serve --host 100.64.0.10 --port 9443' <<<"$unit_overlay"
grep -Fq 'Environment=HUD_HUB_DISCOVERY_ENABLED=0' <<<"$unit_overlay"
grep -Fq 'Environment=HUD_HUB_HTTP_PORT=9443' <<<"$unit_overlay"

unit_lan="$(bash "$INSTALLER" render-unit --lan-auto)"
grep -Fq 'ExecStart=/usr/local/lib/ai-control-hub/ai-control-hub serve --host 0.0.0.0 --port 8787' <<<"$unit_lan"
grep -Fq 'Environment=HUD_HUB_DISCOVERY_ENABLED=1' <<<"$unit_lan"
grep -Fq 'Environment=HUD_HUB_DISCOVERY_PORT=8788' <<<"$unit_lan"
grep -Fq 'Environment=HUD_HUB_HTTP_PORT=8787' <<<"$unit_lan"
grep -Fq 'Environment=HUD_HUB_ID=central-hub' <<<"$unit_lan"

unit_lan_custom="$(bash "$INSTALLER" render-unit --lan-auto --discovery-port 9798 --hub-id dorm-hub)"
grep -Fq 'Environment=HUD_HUB_DISCOVERY_PORT=9798' <<<"$unit_lan_custom"
grep -Fq 'Environment=HUD_HUB_ID=dorm-hub' <<<"$unit_lan_custom"

if bash "$INSTALLER" render-unit --listen 'bad listen' >/dev/null 2>&1; then
  echo "invalid listen address was accepted" >&2
  exit 1
fi
if bash "$INSTALLER" render-unit --lan-auto --listen 192.168.1.10:8787 >/dev/null 2>&1; then
  echo "mutually exclusive LAN/listen modes were accepted" >&2
  exit 1
fi
if bash "$INSTALLER" render-unit --lan-auto --discovery-port 70000 >/dev/null 2>&1; then
  echo "invalid discovery port was accepted" >&2
  exit 1
fi

bash "$LAN_DOCTOR_SMOKE"

echo "[hub-deployment-smoke] ok"
