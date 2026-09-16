# G6 Windows Service + SecretStore

G6 turns the foreground Go backend into a Windows-resident service without changing schema-v1 or the Android server URL.

## Machine layout

The service deliberately separates executable, trusted non-secret configuration, and credentials:

```text
%ProgramFiles%\AI Control HUD\ai-control-agent.exe
%ProgramData%\AIControlHUD\agent.json
%ProgramData%\AIControlHUD\commandcode.dpapi
```

- The installed executable lives under `Program Files` so a LocalSystem service is not launched from a user-writable project directory.
- `agent.json` contains only the listen address and absolute ZCode database paths, but it is still a trusted service input and is ACL-protected.
- `commandcode.dpapi` contains the CommandCode provider record encrypted with Windows DPAPI using machine scope.
- The `%ProgramData%\AIControlHUD` directory and its trusted files use a protected, non-inheriting DACL for `SYSTEM`, `Administrators`, and the user who performed installation. Other local users are not granted access.

No API key is placed in the Windows service command line, environment, Android configuration, machine config, or logs.

## Why absolute ZCode paths are persisted

A Windows service normally runs as LocalSystem. `os.UserHomeDir()` in that process does not refer to the interactive developer profile, so resolving `~/.zcode/...` at service startup would select the wrong profile.

`service install` therefore resolves the current interactive user's ZCode runtime and task-index paths once and persists their absolute paths in machine config. The service opens those SQLite files read-only using the same collectors already validated in G4/G5.

## Commands

Run installation from an elevated PowerShell using the freshly validated G6 executable:

```powershell
.\ai-control-agent.exe service install `
  --provider-config D:\github_programs\ai-control-hud\.local\commandcode-provider.json
```

Optional explicit ZCode paths are available if auto-discovery is not desired:

```powershell
.\ai-control-agent.exe service install `
  --provider-config D:\github_programs\ai-control-hud\.local\commandcode-provider.json `
  --runtime-db C:\Users\you\.zcode\cli\db\db.sqlite `
  --task-index-db C:\Users\you\.zcode\v2\tasks-index.sqlite
```

The first install operation:

1. verifies that at least one ZCode database is readable;
2. reads the existing gitignored provider mirror once;
3. validates that it is the official CommandCode provider and has a key;
4. writes the provider record to DPAPI SecretStore;
5. writes ACL-protected machine config without credentials;
6. copies the current executable into `Program Files`;
7. creates a Windows Defender Firewall inbound rule bound to the installed executable and configured TCP port for `Private` and `Domain` profiles only;
8. registers `AIControlHUD` as an automatic Windows service with bounded restart recovery actions.

The installer deliberately does **not** open the `Public` firewall profile. The existing Android HUD should continue to use a trusted Private LAN or another explicitly trusted path. If a specific overlay adapter is classified as Public on the target machine, handle that adapter policy explicitly rather than globally opening the service on Public networks.

The plaintext import file is **not** deleted automatically. Keep it until the final target-machine service validation is complete. After the service survives restart/boot validation and `doctor --live` succeeds, it can be removed manually.

After a successful first import, reinstall no longer depends on the plaintext mirror. `service remove` preserves machine config and the DPAPI SecretStore. A subsequent `service install` reuses the protected credential when the provider import file is absent, and preserves the stored listen/ZCode paths unless explicit override flags are supplied.

Lifecycle commands:

```powershell
.\ai-control-agent.exe service status
.\ai-control-agent.exe service start
.\ai-control-agent.exe service stop
.\ai-control-agent.exe service restart
.\ai-control-agent.exe service remove
```

`service remove` removes the SCM registration and the service firewall rule but keeps machine config, protected secret, and installed executable so reinstall is reversible. To remove those stored files as well:

```powershell
.\ai-control-agent.exe service remove --purge
```

`--purge` intentionally removes the ability to reinstall without re-importing a credential.

## Doctor

Local checks:

```powershell
.\ai-control-agent.exe doctor
```

The command verifies machine config, configured ZCode paths, DPAPI decryption, official provider identity, and reports service state. It prints no API key or Authorization header.

A bounded live CommandCode request can be added explicitly:

```powershell
.\ai-control-agent.exe doctor --live
```

Only the normalized plan label is printed from the live billing result.

## Foreground troubleshooting

The machine configuration can be run outside SCM while debugging:

```powershell
.\ai-control-agent.exe run --config "$env:ProgramData\AIControlHUD\agent.json"
```

Stop the Windows service first because only one process may own TCP 8787.

The legacy G5 foreground launcher remains a separate rollback path until service validation is complete.

## Service runtime

SCM starts the installed executable as:

```text
ai-control-agent.exe service run --config <machine-config>
```

The platform service adapter converts SCM stop/shutdown requests into context cancellation. The existing HTTP server and collector runtime then perform the same bounded graceful shutdown used in foreground mode.

SCM recovery is configured to restart unexpected failures after 5 seconds, 15 seconds, and 60 seconds; the failure counter resets after one day. Operator-requested stop remains a normal graceful shutdown.

Windows-specific imports are isolated under `agent/internal/platform/*`. A repository architecture test enforces that direct `golang.org/x/sys/windows` imports cannot escape that boundary. Domain, store, API, collectors, and runtime remain platform-neutral.

## Final target-machine gate

Code/CI completion does not close G6. The final gate is intentionally performed last on the target Windows machine:

1. install the G6 artifact from elevated PowerShell;
2. run `doctor --live`;
3. stop the current foreground Go process;
4. start the service and verify local `/api/v1/state` is LIVE;
5. run `scripts/verify-g6-service.ps1` and retain `.local/g6-service.json`;
6. verify the existing Android HUD reconnects unchanged;
7. restart the service and verify Android reconnects;
8. reboot Windows and verify the service starts automatically without an interactive shell;
9. only then remove the old plaintext `.local/commandcode-provider.json` if desired;
10. run `service remove`, reinstall without the plaintext mirror, and start again to prove reversibility.

The verifier checks the SCM service state, `doctor --live`, Go 0.3 server identity, schema v1, overall LIVE, ZCode OK, and CommandCode OK. Android, reboot, and reinstall checks remain explicit because they require the real target environment.
