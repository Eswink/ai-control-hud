# Go Hub transactional binary upgrade

Use this procedure to replace the already-installed CentOS/RHEL Go Hub binary without re-running installation and without re-importing the Hub bearer token.

## What is preserved

`scripts/ai-control-hub-upgrade.sh` changes only:

```text
/usr/local/lib/ai-control-hub/ai-control-hub
```

It does **not** read or rewrite:

- `/etc/ai-control-hud/hub.env` or `HUD_HUB_AGENT_TOKEN`;
- `/var/lib/ai-control-hud/hub.sqlite3` or its event/snapshot data;
- `/etc/systemd/system/ai-control-hub.service`;
- the unit enabled state;
- firewalld rules;
- the installed LAN doctor.

No token file is required for a binary-only upgrade.

## Normal upgrade

Extract a new production Hub bundle and verify its checksums first. Then run:

```bash
bash scripts/ai-control-hub-upgrade.sh \
  --binary "$PWD/ai-control-hub"
```

The candidate must be a separate executable from the installed path and must successfully answer:

```bash
./ai-control-hub version
```

before any service downtime occurs.

## Transaction semantics

For an active service:

1. verify the candidate is executable and `version` succeeds;
2. refuse to proceed if an old `.upgrade.bak` exists;
3. copy the candidate to `ai-control-hub.upgrade.new` with `root:root` mode `0755`;
4. call `sync` while the current Hub is still running;
5. stop `ai-control-hub.service`;
6. rename the installed binary to `ai-control-hub.upgrade.bak`;
7. atomically rename the staged candidate into the installed path;
8. start the service and require `systemctl is-active --quiet` to succeed;
9. remove the backup only after successful activation.

If the new service cannot become active, the helper stops the failed candidate, restores the previous binary and restarts the previous service. The command exits non-zero even when rollback succeeds, making the failed upgrade visible to automation.

For an inactive service, the binary is replaced transactionally but the service remains inactive.

## Recovery guard

A leftover path such as:

```text
/usr/local/lib/ai-control-hub/ai-control-hub.upgrade.bak
```

causes a later upgrade to fail instead of overwriting it. Treat that file as possible recovery evidence from an interrupted prior operation. Inspect the current binary/service state before manually removing or restoring it.

A stale `.upgrade.new` without a `.upgrade.bak` is safe to replace because commit never began.

## Validation after upgrade

For a running Hub:

```bash
systemctl is-active ai-control-hub.service
/usr/local/lib/ai-control-hub/ai-control-hub version
curl -fsS http://127.0.0.1:8787/api/v1/health
```

For a trusted-LAN deployment also run:

```bash
bash scripts/ai-control-hub-systemd.sh lan-doctor
```

This verifies HTTP/UDP listeners and firewalld separately from the binary transaction.

## Security boundary

The helper never prints or parses the Hub bearer token. Configuration and database preservation is structural: those paths are not inputs to the script and are never included in its write set.

Production ownership defaults to `root:root`. Environment overrides for binary path/systemctl/ownership exist only so deterministic CI can exercise the full transaction without touching the runner's real systemd installation.

## CI coverage

`ci-hub-upgrade-smoke.sh` uses a temporary install tree and fake systemctl implementation to prove:

- active successful upgrade preserves active state;
- active candidate startup failure restores the previous binary and active state;
- inactive upgrade stays inactive;
- stale rollback backup is never overwritten;
- synthetic `hub.env`, SQLite and unit sentinel hashes never change;
- staged/backup scratch files are cleaned after success or successful rollback.

The smoke is invoked by the normal Go Hub deployment CI gate and ships independently from production behavior.