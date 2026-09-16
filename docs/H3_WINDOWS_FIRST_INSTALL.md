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

## 3. First service installation

The installer auto-resolves the interactive user's standard ZCode databases and persists their absolute paths for LocalSystem. On first install it also needs a CommandCode provider credential.

CommandCode API keys are **operator-supplied only**. The project must not try to discover, scrape, recover, or auto-retrieve a real CommandCode key from accounts, files, applications, or external services. If the old provider-import file was deleted, create a new local import file yourself using a key you enter/provide directly.

If a valid Windows DPAPI CommandCode SecretStore already exists, it is reused. Otherwise provide the operator-created gitignored provider import file explicitly:

```powershell
.\ai-control-agent.exe service install `
  --provider-config "C:\path\to\ai-control-hud\.local\commandcode-provider.json"
```

Do not paste the provider JSON or its API key into test reports, chat, screenshots, or GitHub.

If ZCode is not in the standard user-profile location, supply absolute paths:

```powershell
.\ai-control-agent.exe service install `
  --provider-config "C:\path\to\ai-control-hud\.local\commandcode-provider.json" `
  --runtime-db "$env:USERPROFILE\.zcode\cli\db\db.sqlite" `
  --task-index-db "$env:USERPROFILE\.zcode\v2\tasks-index.sqlite"
```

A successful install reports the installed executable, machine config, resolved ZCode paths, and whether the DPAPI CommandCode SecretStore was imported or reused. It must not print the API key.

## 4. Validate configuration, then start

```powershell
.\ai-control-agent.exe doctor
.\ai-control-agent.exe doctor --live
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

Before the first Windows upload the Hub intentionally returns HTTP 503 for `/api/v1/state`; after the Agent uploads its first snapshot this endpoint should return schema-v1 state.

## 5. Android interpretation

If Android has discovered the Hub but the Hub still has no primary-agent snapshot, the current client may render the dashboard as offline because `/api/v1/state` returns HTTP 503. Interpret this as "Hub reachable, waiting for first Agent snapshot" when Hub `/api/v1/health` is 200 and Windows service is not yet uploading.

A follow-up Android UX iteration tracks making that distinction explicit in the UI instead of collapsing both cases into `offline`.
