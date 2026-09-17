param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repo = Split-Path -Parent $PSScriptRoot
$agent = Join-Path $repo "agent\ai-control-agent.exe"
$badUpgradeAgent = Join-Path $env:RUNNER_TEMP "ai-control-hud-g6-broken-agent.exe"
$root = Join-Path $env:RUNNER_TEMP "ai-control-hud-g6-service-smoke"
$stateDir = Join-Path $root "state"
$runtimeDb = Join-Path $root "runtime.sqlite"
$taskIndexDb = Join-Path $root "tasks-index.sqlite"
$machineConfig = Join-Path $stateDir "agent.json"
$apiKeyFile = Join-Path $root "commandcode.key"
$port = 18787

function Invoke-SqliteSeed {
    param([string]$Path, [string[]]$Statements)
    $python = @'
import sqlite3, sys
path = sys.argv[1]
statements = sys.argv[2:]
conn = sqlite3.connect(path)
for statement in statements:
    conn.execute(statement)
conn.commit()
conn.close()
'@
    python -c $python $Path @Statements
    if ($LASTEXITCODE -ne 0) {
        throw "failed to seed SQLite database $Path"
    }
}

function Wait-AgentState {
    $deadline = (Get-Date).AddSeconds(25)
    while ((Get-Date) -lt $deadline) {
        try {
            return Invoke-RestMethod -Uri "http://127.0.0.1:$port/api/v1/state" -TimeoutSec 2
        }
        catch {
            Start-Sleep -Milliseconds 400
        }
    }
    throw "agent state endpoint did not become ready"
}

function Assert-ManualCommandCodeProvider {
    $output = & $agent commandcode status --config $machineConfig 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "commandcode status failed: $output"
    }
    $text = ($output -join "`n")
    if ($text -notmatch 'provider=command-code' -or $text -notmatch 'host=api\.commandcode\.ai') {
        throw "unexpected protected CommandCode provider: $text"
    }
    if ($text -match 'commandcode-provider\.json' -or $text -match 'implicit') {
        throw "legacy implicit provider path leaked into configured provider: $text"
    }
}

function Get-ProtectedStateHashes {
    $machine = Get-FileHash -Algorithm SHA256 $machineConfig
    $config = Get-Content $machineConfig -Raw | ConvertFrom-Json
    if (-not (Test-Path $config.commandCodeSecret)) {
        throw "configured DPAPI SecretStore is missing"
    }
    $secret = Get-FileHash -Algorithm SHA256 $config.commandCodeSecret
    return @{
        machine = $machine.Hash
        secret = $secret.Hash
    }
}

function Assert-ProtectedStateHashes {
    param([hashtable]$Before)
    $after = Get-ProtectedStateHashes
    if ($after.machine -ne $Before.machine) {
        throw "machine config changed across binary-only service upgrade"
    }
    if ($after.secret -ne $Before.secret) {
        throw "CommandCode DPAPI SecretStore changed across binary-only service upgrade"
    }
}

function Assert-InstalledBinaryMatchesSource {
    $installed = Join-Path $env:ProgramFiles "AI Control HUD\ai-control-agent.exe"
    if (-not (Test-Path $installed)) {
        throw "installed service binary missing at $installed"
    }
    $sourceHash = (Get-FileHash -Algorithm SHA256 $agent).Hash
    $installedHash = (Get-FileHash -Algorithm SHA256 $installed).Hash
    if ($sourceHash -ne $installedHash) {
        throw "installed service binary hash does not match selected upgrade source"
    }
}

function Assert-UpgradeScratchClean {
    $installed = Join-Path $env:ProgramFiles "AI Control HUD\ai-control-agent.exe"
    foreach ($suffix in @('.upgrade.new', '.upgrade.bak')) {
        if (Test-Path ($installed + $suffix)) {
            throw "upgrade scratch path remains: $installed$suffix"
        }
    }
}

if (Test-Path $root) {
    Remove-Item -Recurse -Force $root
}
New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
Set-Content -NoNewline -Encoding ascii -Path $apiKeyFile -Value "ci-manual-commandcode-key"

$nowMs = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
Invoke-SqliteSeed -Path $runtimeDb -Statements @(
    'CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT NOT NULL, path TEXT, title TEXT NOT NULL, summary_additions INTEGER, summary_deletions INTEGER)',
    'CREATE TABLE session_target (session_id TEXT PRIMARY KEY, target_id TEXT NOT NULL, objective TEXT NOT NULL, status TEXT NOT NULL, token_budget INTEGER, tokens_used INTEGER NOT NULL, time_used_seconds INTEGER NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, summary_title TEXT, active_input_id TEXT, active_run_started_at INTEGER, active_run_last_seen_at INTEGER)',
    'CREATE TABLE todo (session_id TEXT NOT NULL, content TEXT NOT NULL, status TEXT NOT NULL, priority TEXT NOT NULL, position INTEGER NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, PRIMARY KEY (session_id, position))',
    "INSERT INTO session VALUES ('session-ci', 'D:\\ci\\workspace', 'D:\\ci\\workspace', 'CI running task', 1, 0)",
    "INSERT INTO session_target VALUES ('session-ci', 'target-ci', 'CI objective', 'active', NULL, 1, 60, $($nowMs - 60000), $nowMs, 'CI summary', 'input-ci', $($nowMs - 60000), $nowMs)",
    "INSERT INTO todo VALUES ('session-ci', 'CI activity', 'running', 'normal', 0, $nowMs, $nowMs)"
)
Invoke-SqliteSeed -Path $taskIndexDb -Statements @(
    'CREATE TABLE tasks (workspace_key TEXT NOT NULL, workspace_path TEXT NOT NULL, workspace_identity TEXT, task_id TEXT NOT NULL, title TEXT NOT NULL, task_status TEXT, provider TEXT, mode TEXT NOT NULL DEFAULT "", model TEXT, migration_source TEXT, forked_from_task_id TEXT, created_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, unread_at INTEGER, last_unread_at INTEGER NOT NULL DEFAULT 0, pinned INTEGER NOT NULL, archived INTEGER NOT NULL, deleted INTEGER NOT NULL, title_overridden INTEGER NOT NULL DEFAULT 0, meta_json TEXT NOT NULL DEFAULT "{}", searchable_text TEXT NOT NULL DEFAULT "", cron_automation_id TEXT, off_peak_task_id TEXT, PRIMARY KEY (workspace_key, task_id))',
    "INSERT INTO tasks (workspace_key, workspace_path, task_id, title, task_status, updated_at, pinned, archived, deleted) VALUES ('ci', 'D:\\ci\\workspace', 'task-ci', 'CI running task', 'running', $nowMs, 1, 0, 0)"
)

go build -o $badUpgradeAgent .\scripts\fixtures\broken-service-agent.go
if ($LASTEXITCODE -ne 0) {
    throw "failed to build broken service Agent fixture"
}

$installed = $false
try {
    Write-Host "[g6-ci] importing operator-supplied CommandCode key into DPAPI"
    & $agent commandcode configure `
        --config $machineConfig `
        --api-key-file $apiKeyFile
    if ($LASTEXITCODE -ne 0) {
        throw "commandcode configure failed with exit code $LASTEXITCODE"
    }
    Assert-ManualCommandCodeProvider

    Write-Host "[g6-ci] installing Windows service without provider import flag"
    & $agent service install `
        --config $machineConfig `
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
    Assert-ManualCommandCodeProvider

    Write-Host "[g6-ci] deleting operator plaintext key after protected-store validation"
    Remove-Item -Force $apiKeyFile
    if (Test-Path $apiKeyFile) {
        throw "plaintext CommandCode key still exists after deletion"
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

    Write-Host "[g6-ci] upgrading while service is running"
    $protectedBeforeUpgrade = Get-ProtectedStateHashes
    & $agent service upgrade --config $machineConfig --source $agent
    if ($LASTEXITCODE -ne 0) {
        throw "running service upgrade failed with exit code $LASTEXITCODE"
    }
    $service = Get-Service -Name "AIControlHUD" -ErrorAction Stop
    if ($service.Status -ne "Running") {
        throw "service did not return to running state after upgrade: $($service.Status)"
    }
    Assert-ProtectedStateHashes $protectedBeforeUpgrade
    Assert-InstalledBinaryMatchesSource
    Assert-UpgradeScratchClean
    $state = Wait-AgentState
    if ($state.zcode.health.status -ne "ok") {
        throw "ZCode did not recover after running service upgrade"
    }

    Write-Host "[g6-ci] forcing post-preflight SCM failure and verifying automatic rollback"
    $protectedBeforeRollback = Get-ProtectedStateHashes
    & $agent service upgrade --config $machineConfig --source $badUpgradeAgent
    if ($LASTEXITCODE -eq 0) {
        throw "broken service candidate unexpectedly succeeded"
    }
    $service = Get-Service -Name "AIControlHUD" -ErrorAction Stop
    if ($service.Status -ne "Running") {
        throw "old service was not restored to running after failed upgrade: $($service.Status)"
    }
    Assert-ProtectedStateHashes $protectedBeforeRollback
    Assert-InstalledBinaryMatchesSource
    Assert-UpgradeScratchClean
    $state = Wait-AgentState
    if ($state.zcode.health.status -ne "ok") {
        throw "old service did not recover after failed upgrade rollback"
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

    Write-Host "[g6-ci] upgrading while service is stopped"
    $protectedBeforeStoppedUpgrade = Get-ProtectedStateHashes
    & $agent service upgrade --config $machineConfig --source $agent
    if ($LASTEXITCODE -ne 0) {
        throw "stopped service upgrade failed with exit code $LASTEXITCODE"
    }
    $service = Get-Service -Name "AIControlHUD" -ErrorAction Stop
    if ($service.Status -ne "Stopped") {
        throw "stopped service was unexpectedly started by upgrade: $($service.Status)"
    }
    Assert-ProtectedStateHashes $protectedBeforeStoppedUpgrade
    Assert-InstalledBinaryMatchesSource
    Assert-UpgradeScratchClean

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

    Write-Host "[g6-ci] reinstalling service from protected SecretStore only"
    & $agent service install --config $machineConfig
    if ($LASTEXITCODE -ne 0) {
        throw "service reinstall failed with exit code $LASTEXITCODE"
    }
    $installed = $true
    Assert-ManualCommandCodeProvider
    Write-Host "[g6-ci] final purge"
    & $agent service remove --config $machineConfig --purge
    if ($LASTEXITCODE -ne 0) {
        throw "service purge failed with exit code $LASTEXITCODE"
    }
    $installed = $false
    if (Test-Path $machineConfig) {
        throw "machine config still exists after purge"
    }
    Write-Host "[g6-ci] PASSED"
}
finally {
    if ($installed) {
        & $agent service remove --config $machineConfig --purge | Out-Host
    }
    Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
    Remove-Item -Force $badUpgradeAgent -ErrorAction SilentlyContinue
}
