param(
    [string]$AgentPath = ".local\bin\ai-control-agent.exe",
    [int]$PythonPort = 8797,
    [int]$GoPort = 8798,
    [string]$WorkDir = ".local\g4-parity"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$python = Join-Path $repoRoot ".venv\Scripts\python.exe"
if (-not (Test-Path $python)) {
    throw "Python virtual environment not found at .venv."
}

$agent = if ([System.IO.Path]::IsPathRooted($AgentPath)) {
    $AgentPath
} else {
    Join-Path $repoRoot $AgentPath
}
if (-not (Test-Path $agent)) {
    throw "Go agent not found at $agent."
}
$agent = (Resolve-Path $agent).Path

$work = if ([System.IO.Path]::IsPathRooted($WorkDir)) {
    $WorkDir
} else {
    Join-Path $repoRoot $WorkDir
}
New-Item -ItemType Directory -Force $work | Out-Null

$runtimeDb = Join-Path $work "runtime.sqlite"
$taskDb = Join-Path $work "tasks-index.sqlite"
$runtimeOffline = "$runtimeDb.offline"
$taskOffline = "$taskDb.offline"
$disabledProvider = Join-Path $work "disabled-provider.json"
$pythonOut = Join-Path $work "python.stdout.log"
$pythonErr = Join-Path $work "python.stderr.log"
$goOut = Join-Path $work "go.stdout.log"
$goErr = Join-Path $work "go.stderr.log"
$summaryPath = Join-Path $work "summary.json"

Remove-Item $runtimeOffline, $taskOffline, $pythonOut, $pythonErr, $goOut, $goErr -Force -ErrorAction SilentlyContinue
[System.IO.File]::WriteAllText($disabledProvider, '{"provider":{}}', [System.Text.UTF8Encoding]::new($false))

function Get-State([string]$Base) {
    Invoke-RestMethod -Uri ($Base.TrimEnd('/') + "/api/v1/state") -Method Get -TimeoutSec 3
}

function Wait-State([string]$Base, [string]$Description, [scriptblock]$Predicate, [int]$TimeoutSec = 20) {
    $deadline = (Get-Date).AddSeconds($TimeoutSec)
    do {
        try {
            $state = Get-State $Base
            if (& $Predicate $state) {
                return $state
            }
        }
        catch {
            # The child may still be starting or transitioning.
        }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw "Timed out waiting for $Description at $Base"
}

function Move-WithRetry([string]$Source, [string]$Destination) {
    $last = $null
    for ($i = 0; $i -lt 30; $i++) {
        try {
            Move-Item -LiteralPath $Source -Destination $Destination -Force
            return
        }
        catch {
            $last = $_
            Start-Sleep -Milliseconds 100
        }
    }
    throw "Could not move $Source to $Destination after retries: $last"
}

$pythonBase = "http://127.0.0.1:$PythonPort"
$goBase = "http://127.0.0.1:$GoPort"
$compareScript = Join-Path $repoRoot "scripts\compare-shadow.ps1"

function Compare-Stage([string]$Stage) {
    $output = Join-Path $work ("compare-{0}.json" -f $Stage)
    & $compareScript -PythonBase $pythonBase -GoBase $goBase -OutputPath $output | Out-Host
    $report = Get-Content -LiteralPath $output -Raw | ConvertFrom-Json
    $failed = @($report.exactChecks.PSObject.Properties | Where-Object { -not [bool]$_.Value })
    if ($failed.Count -gt 0) {
        $names = ($failed | ForEach-Object Name) -join ", "
        throw "G4 $Stage parity failed: $names"
    }
    return $output
}

$envNames = @(
    "HUD_ZCODE_RUNTIME_DB",
    "HUD_ZCODE_DB",
    "HUD_ZCODE_CONFIG",
    "HUD_FIXTURE",
    "HUD_HOST",
    "HUD_PORT",
    "AI_CONTROL_FIXTURE"
)
$previousEnv = @{}
foreach ($name in $envNames) {
    $previousEnv[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
}

$pythonProcess = $null
$goProcess = $null
$stageOutputs = [ordered]@{}

try {
    & $python (Join-Path $repoRoot "scripts\g4_state_db.py") init --runtime $runtimeDb --task-index $taskDb
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to initialize synthetic G4 databases."
    }

    $env:HUD_ZCODE_RUNTIME_DB = $runtimeDb
    $env:HUD_ZCODE_DB = $taskDb
    $env:HUD_ZCODE_CONFIG = $disabledProvider
    Remove-Item Env:HUD_FIXTURE -ErrorAction SilentlyContinue
    Remove-Item Env:AI_CONTROL_FIXTURE -ErrorAction SilentlyContinue
    $env:HUD_HOST = "127.0.0.1"
    $env:HUD_PORT = [string]$PythonPort

    $pythonProcess = Start-Process -FilePath $python -ArgumentList @("-m", "server") -WorkingDirectory $repoRoot -PassThru -RedirectStandardOutput $pythonOut -RedirectStandardError $pythonErr
    $goProcess = Start-Process -FilePath $agent -ArgumentList @("run", "-listen", "127.0.0.1:$GoPort") -WorkingDirectory $repoRoot -PassThru -RedirectStandardOutput $goOut -RedirectStandardError $goErr

    Wait-State $pythonBase "Python synthetic running state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 1 } | Out-Null
    Wait-State $goBase "Go synthetic running state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 1 } | Out-Null
    $stageOutputs.running = Compare-Stage "running"
    Write-Host "[g4] running parity passed"

    Move-WithRetry $runtimeDb $runtimeOffline
    Move-WithRetry $taskDb $taskOffline
    Wait-State $pythonBase "Python stale/LKG state" { param($s) $s.zcode.health.status -eq "stale" -and $s.zcode.summary.running -eq 1 } | Out-Null
    Wait-State $goBase "Go stale/LKG state" { param($s) $s.zcode.health.status -eq "stale" -and $s.zcode.summary.running -eq 1 } | Out-Null
    $stageOutputs.stale = Compare-Stage "stale"
    Write-Host "[g4] source failure -> stale/LKG parity passed"

    Move-WithRetry $runtimeOffline $runtimeDb
    Move-WithRetry $taskOffline $taskDb
    Wait-State $pythonBase "Python recovered state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 1 } | Out-Null
    Wait-State $goBase "Go recovered state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 1 } | Out-Null
    $stageOutputs.recovered = Compare-Stage "recovered"
    Write-Host "[g4] recovery parity passed"

    & $python (Join-Path $repoRoot "scripts\g4_state_db.py") complete --runtime $runtimeDb
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to transition synthetic Goal to completed."
    }
    Wait-State $pythonBase "Python completed Goal" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.completed -eq 1 -and $s.zcode.tasks[0].status -eq "completed" } | Out-Null
    Wait-State $goBase "Go completed Goal" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.completed -eq 1 -and $s.zcode.tasks[0].status -eq "completed" } | Out-Null
    $stageOutputs.completed = Compare-Stage "completed"
    Write-Host "[g4] running -> completed parity passed"

    & $python (Join-Path $repoRoot "scripts\g4_state_db.py") idle --runtime $runtimeDb
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to transition synthetic Goal to idle."
    }
    Wait-State $pythonBase "Python idle state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 0 -and $s.zcode.summary.completed -eq 0 -and @($s.zcode.tasks).Count -eq 0 } | Out-Null
    Wait-State $goBase "Go idle state" { param($s) $s.zcode.health.status -eq "ok" -and $s.zcode.summary.running -eq 0 -and $s.zcode.summary.completed -eq 0 -and @($s.zcode.tasks).Count -eq 0 } | Out-Null
    $stageOutputs.idle = Compare-Stage "idle"
    Write-Host "[g4] terminal -> idle parity passed"

    $summary = [ordered]@{
        generatedAt = (Get-Date).ToUniversalTime().ToString("o")
        result = "passed"
        isolatedPorts = [ordered]@{ python = $PythonPort; go = $GoPort }
        stages = $stageOutputs
        productionEndpointsTouched = $false
        realProviderUsed = $false
    }
    $json = $summary | ConvertTo-Json -Depth 6
    [System.IO.File]::WriteAllText($summaryPath, $json, [System.Text.UTF8Encoding]::new($false))
    Write-Host "[g4] deterministic lifecycle parity PASSED"
    Write-Host "[g4] summary: $summaryPath"
}
finally {
    if (Test-Path $runtimeOffline) {
        Move-Item -LiteralPath $runtimeOffline -Destination $runtimeDb -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path $taskOffline) {
        Move-Item -LiteralPath $taskOffline -Destination $taskDb -Force -ErrorAction SilentlyContinue
    }
    foreach ($process in @($goProcess, $pythonProcess)) {
        if ($null -ne $process) {
            try {
                if (-not $process.HasExited) {
                    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
                    $process.WaitForExit(3000) | Out-Null
                }
            }
            catch {
                # Cleanup is best effort.
            }
        }
    }
    foreach ($name in $envNames) {
        $value = $previousEnv[$name]
        if ($null -eq $value) {
            Remove-Item ("Env:" + $name) -ErrorAction SilentlyContinue
        }
        else {
            [Environment]::SetEnvironmentVariable($name, [string]$value, "Process")
        }
    }
}
