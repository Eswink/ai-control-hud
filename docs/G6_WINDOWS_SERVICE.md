# G6 Windows Service + SecretStore

The production Windows Agent runs as an SCM service without changing the schema-v1 Android/Hub contract.

## Machine layout

```text
%ProgramFiles%\AI Control HUD\ai-control-agent.exe
%ProgramData%\AIControlHUD\agent.json
%ProgramData%\AIControlHUD\commandcode.dpapi
%ProgramData%\AIControlHUD\hub.dpapi
```

- the executable lives under `Program Files`;
- `agent.json` contains trusted non-secret service configuration and absolute ZCode paths;
- `commandcode.dpapi` stores the operator-supplied CommandCode provider credential using machine-scope DPAPI;
- `hub.dpapi` stores the independent Hub ingest credential using the same protected platform store;
- `%ProgramData%\AIControlHUD` uses a protected DACL for SYSTEM, Administrators, and the installing user.

No API key or Hub token is placed in the SCM command line, Android configuration, or logs.

## CommandCode credential bootstrap

CommandCode API keys are supplied manually by the operator. The project does not discover, scrape, recover, or auto-retrieve a real key from ZCode, accounts, files, applications, or external services.

Create a temporary one-line file yourself, then import it:

```powershell
.\ai-control-agent.exe commandcode configure `
  --api-key-file C:\Temp\commandcode.key

.\ai-control-agent.exe commandcode status
```

The protected record is fixed to the verified official `api.commandcode.ai` provider and the key value is never printed.

The older `--provider-config PATH` path remains only as an **explicit** compatibility/migration import. Merely having `.local\commandcode-provider.json` or a ZCode provider config present does not cause machine/service credential import.

## First service install

A normal first install reuses the already-protected CommandCode SecretStore:

```powershell
.\ai-control-agent.exe service install
```

Optional explicit ZCode paths:

```powershell
.\ai-control-agent.exe service install `
  --runtime-db "$env:USERPROFILE\.zcode\cli\db\db.sqlite" `
  --task-index-db "$env:USERPROFILE\.zcode\v2\tasks-index.sqlite"
```

`service install`:

1. verifies at least one ZCode database is readable;
2. resolves and persists absolute source paths for LocalSystem;
3. adds the minimum SYSTEM read/traverse ACLs required for those SQLite sources;
4. reuses the protected CommandCode SecretStore unless an explicit `--provider-config` is supplied;
5. writes ACL-protected machine config without credentials;
6. copies the Agent into `Program Files`;
7. creates a Windows Firewall rule scoped to Private/Domain profiles;
8. registers automatic SCM service `AIControlHUD` with bounded restart recovery actions.

Validate before deleting the temporary key file:

```powershell
.\ai-control-agent.exe doctor
.\ai-control-agent.exe doctor --live
Remove-Item C:\Temp\commandcode.key
```

The service never needs that plaintext file again.

## Lifecycle commands

```powershell
.\ai-control-agent.exe service status
.\ai-control-agent.exe service start
.\ai-control-agent.exe service stop
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service upgrade
.\ai-control-agent.exe service remove
```

`service remove` removes SCM registration and the firewall rule while preserving machine config, protected credentials, and the installed binary so reinstall is reversible.

To remove stored state too:

```powershell
.\ai-control-agent.exe service remove --purge
```

The additive SYSTEM read ACEs on ZCode source paths are intentionally retained because the installer cannot safely distinguish pre-existing entries from entries it added.

## Transactional binary upgrade

To update an already-installed Windows Agent, run the **newly downloaded/validated** executable from outside the installed `Program Files` target:

```powershell
.\ai-control-agent-new.exe service upgrade
```

For controlled automation a source path can be explicit:

```powershell
.\ai-control-agent.exe service upgrade --source C:\Temp\ai-control-agent-new.exe
```

The normal operator path should run the new executable directly. Do not invoke the installed target as both the running CLI and replacement source.

Upgrade semantics:

1. require an already-installed service in `running` or `stopped` state;
2. copy and fsync the candidate to `ai-control-agent.exe.upgrade.new` **before** service downtime;
3. if the service was running, stop it cleanly;
4. rename the old binary to `ai-control-agent.exe.upgrade.bak`;
5. atomically move the staged candidate into the installed path;
6. if the service was previously running, start it and require stable SCM `Running` state;
7. on candidate startup failure, stop the failed candidate, restore the old binary, and restart the old service;
8. after success remove the backup; after rollback remove upgrade scratch state.

A service that was stopped before upgrade remains stopped.

The upgrade operation **does not** rewrite or re-import:

- `%ProgramData%\AIControlHUD\agent.json`;
- CommandCode DPAPI SecretStore;
- Hub DPAPI SecretStore;
- ZCode source paths/ACLs;
- Windows Firewall rule;
- SCM registration/recovery configuration.

If a stale `.upgrade.bak` exists, the command refuses to overwrite it. This is deliberate: an unclean prior upgrade may have left the last known-good executable there.

## Doctor

```powershell
.\ai-control-agent.exe doctor
.\ai-control-agent.exe doctor --live
```

`doctor` checks machine config, ZCode paths, protected CommandCode credential, provider identity, and service state. `--live` adds one bounded CommandCode billing request and prints only normalized non-secret results.

## Foreground troubleshooting

Stop SCM first, then:

```powershell
.\ai-control-agent.exe run --config "$env:ProgramData\AIControlHUD\agent.json"
```

Only one process can own the configured local HTTP port.

## Service runtime and recovery

SCM launches:

```text
ai-control-agent.exe service run --config <machine-config>
```

SCM stop/shutdown requests become context cancellation so HTTP/collector/uploader loops perform bounded graceful shutdown. Unexpected failures use configured restart delays of 5 seconds, 15 seconds, and 60 seconds; the counter resets after one day.

Windows-specific imports remain isolated under `agent/internal/platform/*`.

## CI coverage

Windows hosted CI performs real SCM operations with synthetic ZCode state and non-production credentials. Current smoke coverage includes:

- operator key-file import into real Windows DPAPI;
- historical implicit provider-file canary ignored;
- service install/start/restart/stop;
- running-service transactional upgrade;
- machine config and DPAPI SHA-256 unchanged by upgrade;
- installed binary equals the selected upgrade source;
- deliberately invalid executable candidate causes real SCM startup failure, automatic binary rollback, and old-service recovery;
- stopped-service upgrade preserves stopped state;
- remove while preserving config/DPAPI;
- reinstall from protected SecretStore with no plaintext provider/key file;
- final purge.

This CI path exercises the same executable replacement and SCM waits used by production.

## Target-machine status

The accepted real Windows/Android deployment has already validated first service installation, protected CommandCode credential use, live billing, Windows-to-Hub upload, Android dashboard/TTS, and stop -> Hub stale/degraded -> restart recovery.

Additional disaster/endurance drills are recorded separately as `NOT RUN` by operator choice and do not block continued development.
