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
$machineConfig = Join-Path $root "agent.json"
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
        "apiKey": "cc-ci-g6-smoke-secret"
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
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $state = Invoke-RestMethod -Uri "$baseUrl/api/v1/state" -Method Get -TimeoutSec 3
            if ($state.schemaVersion -eq 1 -and
                [string]$state.server.version -like "0.3.*-go*" -and
                $state.zcode.health.status -eq "ok") {
                return $state
            }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 500
    }
    throw "G6 service did not become healthy in time. Last error: $lastError"
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

    Write-Host "[g6-ci] Windows SCM + DPAPI smoke PASSED"
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
