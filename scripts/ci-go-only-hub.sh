#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "[go-only-hub] $*" >&2
  exit 1
}

for path in \
  server/hub \
  server/hub_app.py \
  tools/hub_backup.py \
  scripts/backup_hub_db.py
do
  if [[ -e "$path" ]]; then
    fail "retired Python Hub path still exists: $path"
  fi
done

if grep -Fq 'ai-control-hub-backup' pyproject.toml; then
  fail "retired Python Hub backup console script is still registered"
fi

for file in .github/workflows/*.yml; do
  if grep -Eq 'server/hub|server\.hub|hub_app\.py|tools/hub_backup\.py|backup_hub_db\.py|ai-control-hub-backup' "$file"; then
    if [[ "$file" != ".github/workflows/go-hub.yml" ]]; then
      fail "retired Python Hub path is referenced by $file"
    fi
  fi
done

echo "[go-only-hub] ok: standalone Go Hub is the only Central Hub runtime"
