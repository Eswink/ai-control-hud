#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="$ROOT/scripts/ai-control-hub-systemd.sh"

bash -n "$INSTALLER"

unit_default="$(bash "$INSTALLER" render-unit)"
grep -Fq -- '--host 127.0.0.1 --port 8787' <<<"$unit_default"
grep -Fq 'User=ai-control-hub' <<<"$unit_default"
grep -Fq 'EnvironmentFile=/etc/ai-control-hud/hub.env' <<<"$unit_default"
grep -Fq 'NoNewPrivileges=true' <<<"$unit_default"
grep -Fq 'ProtectSystem=strict' <<<"$unit_default"
grep -Fq 'ProtectHome=true' <<<"$unit_default"
grep -Fq 'CapabilityBoundingSet=' <<<"$unit_default"
grep -Fq 'ReadWritePaths=/var/lib/ai-control-hud' <<<"$unit_default"

if grep -Fq 'HUD_HUB_AGENT_TOKEN=' <<<"$unit_default"; then
  echo "rendered unit must not contain the bearer token" >&2
  exit 1
fi

unit_overlay="$(bash "$INSTALLER" render-unit --listen 100.64.0.10:9443)"
grep -Fq -- '--host 100.64.0.10 --port 9443' <<<"$unit_overlay"

if bash "$INSTALLER" render-unit --listen 'bad listen' >/dev/null 2>&1; then
  echo "invalid listen address was accepted" >&2
  exit 1
fi

echo "[hub-deployment-smoke] ok"
