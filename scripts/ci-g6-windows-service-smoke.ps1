$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$agent = Join-Path $repoRoot "agent\ai-control-agent.exe"
if (-not (Test-Path $agent)) {
    throw "G6 CI agent executable not found at $agent"
}

$root = Join-Path $env:RUNNER_TEMP "ai-control-hud-g6-service-smoke"
$runtimeDb = Join-Path $root "runtime.sqlite"
$taskIndexDb = Join-Path $root "tasks-index.sqlite"
$providerConfig = Join-Path $root "provider.json"
$machineConfig = Join-Path $root "state\agent.json"
$port = 18787
$baseUrl = "http://127.0.0.1:$port"

if (Test-Path $root) {
    Remove-Item -Recurse -Force $root
}
New-Item -ItemType Directory -Force $root | Out-Null

$pythonCommand = Get-Command python -ErrorAction SilentlyContinue
if ($null -eq $pythonCommand) {
    throw "Python is required for the G6 synthetic SQLite smoke fixture"
}

& $pythonCommand.Source (Join-Path $repoRoot "scripts\g4_state_db.py") init `
    --runtime $runtimeDb `
    --task-index $taskIndexDb
if ($LASTEXITCODE -ne 0) {
    throw "Failed to create synthetic G6 ZCode databases"
}

$providerJson = @'
{
  "provider": {
    "command": {
      "name": "command",
      "kind": "openai",
      "enabled": true,
      "options": {
        "baseURL": "https://api.commandcode.ai/provider/v1",
        "apiKey": "test-only"
      },
      "models": {}
    }
  }
}
'@
[System.IO.File]::WriteAllText($providerConfig, $providerJson, [System.Text.UTF8Encoding]::new($false))

function Wait-AgentState {
    param([int]$TimeoutSeconds = 30)

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $lastError = $null
    $lastState = $null
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $lastState = Invoke-RestMethod -Uri "$baseUrl/api/v1/state" -Method Get -TimeoutSec 3
            if ($lastState.schemaVersion -eq 1 -and
                [string]$lastState.server.version -like "0.3.*-go*" -and
                $lastState.zcode.health.status -eq "ok") {
                return $lastState
            }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 500
    }

    if ($null -ne $lastState) {
        $diagnostic = [ordered]@{
            serverVersion = $lastState.server.version
            overall = $lastState.overall.status
            zcodeHealth = $lastState.zcode.health
            zcodeSummary = $lastState.zcode.summary
            commandCodeHealth = $lastState.commandCode.health
        } | ConvertTo-Json -Depth 8 -Compress
        Write-Host "[g6-ci] last state: $diagnostic"
    }
    & $agent service status
    $service = Get-Service -Name "AIControlHUD" -ErrorAction SilentlyContinue
    if ($null -ne $service) {
        Write-Host "[g6-ci] SCM state=$($service.Status)"
    }
    throw "G6 service did not become healthy in time. Last request error: $lastError"
}

$installed = $false
try {
    Write-Host "[g6-ci] installing Windows service"
    & $agent service install `
        --config $machineConfig `
        --provider-config $providerConfig `
        --runtime-db $runtimeDb `
        --task-index-db $taskIndexDb `
        --listen "127.0.0.1:$port"
    if ($LASTEXITCODE -ne 0) {
        throw "service install failed with exit code $LASTEXITCODE"
    }
    $installed = $true

    Write-Host "[g6-ci] verifying DPAPI/config in installer context"
    & $agent doctor --config $machineConfig
    if ($LASTEXITCODE -ne 0) {
        throw "doctor failed with exit code $LASTEXITCODE"
    }

    Write-Host "[g6-ci] starting LocalSystem service"
    & $agent service start
    if ($LASTEXITCODE -ne 0) {
        throw "service start failed with exit code $LASTEXITCODE"
    }
    $state = Wait-AgentState
    if ($state.zcode.summary.running -ne 1) {
        throw "unexpected synthetic ZCode running count: $($state.zcode.summary.running)"
    }

    Write-Host "[g6-ci] restarting service"
    & $agent service restart
    if ($LASTEXITCODE -ne 0) {
        throw "service restart failed with exit code $LASTEXITCODE"
    }
    $state = Wait-AgentState
    if ($state.zcode.health.status -ne "ok") {
        throw "ZCode did not recover after service restart"
    }

    Write-Host "[g6-ci] stopping service"
    & $agent service stop
    if ($LASTEXITCODE -ne 0) {
        throw "service stop failed with exit code $LASTEXITCODE"
    }
    $service = Get-Service -Name "AIControlHUD" -ErrorAction Stop
    if ($service.Status -ne "Stopped") {
        throw "service status after stop is $($service.Status)"
    }

    Write-Host "[g6-ci] removing service while preserving machine state"
    & $agent service remove --config $machineConfig
    if ($LASTEXITCODE -ne 0) {
        throw "service remove failed with exit code $LASTEXITCODE"
    }
    $installed = $false
    if (-not (Test-Path $machineConfig)) {
        throw "machine config was unexpectedly removed"
    }
    $machineState = Get-Content $machineConfig -Raw | ConvertFrom-Json
    if (-not (Test-Path $machineState.commandCodeSecret)) {
        throw "DPAPI SecretStore was unexpectedly removed"
    }

    Write-Host "[g6-ci] deleting plaintext provider import before reinstall"
    Remove-Item -Force $providerConfig
    if (Test-Path $providerConfig) {
        throw "plaintext provider import still exists after deletion"
    }

    Write-Host "[g6-ci] reinstalling from preserved DPAPI SecretStore"
    & $agent service install `
        --config $machineConfig `
        --provider-config $providerConfig
    if ($LASTEXITCODE -ne 0) {
        throw "service reinstall without plaintext provider failed with exit code $LASTEXITCODE"
    }
    $installed = $true

    & $agent doctor --config $machineConfig
    if ($LASTEXITCODE -ne 0) {
        throw "doctor after reinstall failed with exit code $LASTEXITCODE"
    }

    & $agent service start
    if ($LASTEXITCODE -ne 0) {
        throw "service start after reinstall failed with exit code $LASTEXITCODE"
    }
    $state = Wait-AgentState
    if ($state.zcode.summary.running -ne 1) {
        throw "unexpected ZCode state after reinstall"
    }

    Write-Host "[g6-ci] stopping reinstalled service"
    & $agent service stop
    if ($LASTEXITCODE -ne 0) {
        throw "service stop after reinstall failed with exit code $LASTEXITCODE"
    }

    Write-Host "[g6-ci] Windows SCM + DPAPI + reinstall smoke PASSED"
} finally {
    if ($installed) {
        try {
            & $agent service remove --config $machineConfig --purge
        } catch {
            Write-Warning "G6 CI cleanup command failed: $($_.Exception.Message)"
        }
    }
    try {
        $leftover = Get-Service -Name "AIControlHUD" -ErrorAction SilentlyContinue
        if ($null -ne $leftover) {
            sc.exe stop AIControlHUD | Out-Null
            sc.exe delete AIControlHUD | Out-Null
        }
    } catch {
        Write-Warning "G6 CI fallback service cleanup failed: $($_.Exception.Message)"
    }
    if (Test-Path $root) {
        Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
    }
}
