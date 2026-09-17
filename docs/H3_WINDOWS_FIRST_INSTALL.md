# H3 Windows first-install gate

Use this gate before `service restart` during real-device validation or a fresh Windows deployment. A machine may have a protected Hub credential but no `AIControlHUD` SCM service yet.

## 1. Confirm Hub discovery

From an elevated PowerShell:

```powershell
.\ai-control-agent.exe hub status
```

Expected auto mode:

```text
configured=true agent=desktop-main url=auto://lan resolved=http://<CURRENT_HUB_IP>:8787 hub=dorm-hub
```

This proves Hub discovery only. It does **not** prove the Agent service is installed or uploading state.

## 2. Check service state before using restart

```powershell
.\ai-control-agent.exe service status
```

If it reports `installed=true`, use `service restart` and continue with the normal deployment checks.

If it reports:

```text
installed=false state=not-installed
```

this is a first-install path. Do not call `service restart` yet.

## 3. Import the operator-supplied CommandCode key

CommandCode API keys are **operator-supplied only**. The project does not discover, scrape, recover, or auto-retrieve a real CommandCode key from ZCode, accounts, files, applications, or external services.

The preferred first-install path is a one-line temporary file created by the operator. For example, create `C:\Temp\commandcode.key` yourself and place only the API key in that file. Do not paste the key into chat, GitHub, screenshots, or a command-line argument.

Import it directly into the platform SecretStore:

```powershell
.\ai-control-agent.exe commandcode configure `
  --api-key-file C:\Temp\commandcode.key

.\ai-control-agent.exe commandcode status
```

Expected status is similar to:

```text
configured=true provider=command-code host=api.commandcode.ai secret=C:\ProgramData\AIControlHUD\commandcode.dpapi
```

The command fixes the provider metadata to the verified official Command Code endpoint and stores the key in the platform SecretStore. On Windows that store is machine-scope DPAPI protected. The key value is never printed.

The older explicit provider-JSON import remains available for compatibility:

```powershell
.\ai-control-agent.exe service install `
  --provider-config "C:\path\to\commandcode-provider.json"
```

but it is no longer the recommended way to enter a new key.

## 4. Install the Windows service

The installer auto-resolves the interactive user's standard ZCode databases and persists their absolute paths for LocalSystem. Once `commandcode status` is configured, a normal first install can reuse that SecretStore directly:

```powershell
.\ai-control-agent.exe service install
```

If ZCode is not in the standard user-profile location, supply absolute paths:

```powershell
.\ai-control-agent.exe service install `
  --runtime-db "$env:USERPROFILE\.zcode\cli\db\db.sqlite" `
  --task-index-db "$env:USERPROFILE\.zcode\v2\tasks-index.sqlite"
```

A successful install reports the installed executable, machine config, resolved ZCode paths, and that the existing protected SecretStore was reused. It must not print the API key.

## 5. Validate configuration, then remove the plaintext key file

```powershell
.\ai-control-agent.exe doctor
.\ai-control-agent.exe doctor --live
```

Only after those checks succeed, delete the temporary plaintext key file:

```powershell
Remove-Item C:\Temp\commandcode.key
```

Then start the service:

```powershell
.\ai-control-agent.exe service start
.\ai-control-agent.exe service status
```

If startup fails because TCP 8787 is already in use, stop the old foreground Agent process before starting the SCM service.

Local checks:

```powershell
curl.exe http://127.0.0.1:8787/api/v1/health
curl.exe http://127.0.0.1:8787/api/v1/state
```

Then verify on the Hub:

```bash
curl -fsS http://127.0.0.1:8787/api/v1/state
```

Before the first Windows upload the Hub intentionally returns HTTP 503 for `/api/v1/state`; after the Agent uploads its first snapshot this endpoint returns schema-v1 state.

## 6. Android interpretation

When the Hub is reachable but has not yet received the first primary-Agent snapshot, current Android builds display:

```text
WAITING FOR AGENT
```

This is distinct from transport `OFFLINE`. Once the Windows Agent uploads its first snapshot, Android returns to `LIVE` or `DEGRADED` according to source health.

## 7. Credential lifecycle

Inspect the protected credential without revealing the key:

```powershell
.\ai-control-agent.exe commandcode status
```

To intentionally remove it:

```powershell
.\ai-control-agent.exe commandcode remove
```

Removing the SecretStore makes configured service collection fail until a new operator-supplied key is imported. The command never tries to recover the old key from another source.
