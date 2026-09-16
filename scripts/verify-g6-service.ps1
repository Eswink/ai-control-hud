param(
    [string]$AgentPath = "$env:ProgramFiles\AI Control HUD\ai-control-agent.exe",
    [string]$BaseUrl = "http://127.0.0.1:8787",
    [string]$OutputPath = ".local\g6-service.json"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

if (-not (Test-Path $AgentPath)) {
    throw "Installed G6 agent not found at $AgentPath"
}

Write-Host "[g6] running doctor --live"
& $AgentPath doctor --live
if ($LASTEXITCODE -ne 0) {
    throw "G6 doctor --live failed with exit code $LASTEXITCODE"
}

$service = Get-Service -Name "AIControlHUD" -ErrorAction Stop
if ($service.Status -ne "Running") {
    throw "AIControlHUD service is not running (state=$($service.Status))"
}

$state = Invoke-RestMethod -Uri ($BaseUrl.TrimEnd('/') + "/api/v1/state") -Method Get -TimeoutSec 8

$checks = [ordered]@{
    schemaVersion = ($state.schemaVersion -eq 1)
    goServer = ([string]$state.server.version -like "0.3.*-go*")
    overallLive = ($state.overall.status -eq "live")
    zcodeOK = ($state.zcode.health.status -eq "ok")
    commandCodeOK = ($state.commandCode.health.status -eq "ok")
}

$failed = @($checks.GetEnumerator() | Where-Object { -not $_.Value } | ForEach-Object { $_.Key })
if ($failed.Count -gt 0) {
    throw "G6 service verification failed: $($failed -join ', ')"
}

$firstTask = $null
if ($null -ne $state.zcode.tasks -and $state.zcode.tasks.Count -gt 0) {
    $firstTask = $state.zcode.tasks[0]
}

$report = [ordered]@{
    generatedAt = (Get-Date).ToUniversalTime().ToString("o")
    endpoint = $BaseUrl
    result = "passed"
    service = [ordered]@{
        name = "AIControlHUD"
        status = [string]$service.Status
    }
    checks = $checks
    server = [ordered]@{
        version = $state.server.version
        uptimeSeconds = $state.server.uptimeSeconds
    }
    zcode = [ordered]@{
        health = $state.zcode.health.status
        summary = $state.zcode.summary
        firstTask = $firstTask
    }
    commandCode = [ordered]@{
        health = $state.commandCode.health.status
        plan = $state.commandCode.usage.plan
        credit = $state.commandCode.usage.credit
        windows = $state.commandCode.usage.windows
    }
    androidVerificationRequired = $true
    rebootVerificationRequired = $true
    reinstallVerificationRequired = $true
}

$json = $report | ConvertTo-Json -Depth 12
$target = if ([System.IO.Path]::IsPathRooted($OutputPath)) { $OutputPath } else { Join-Path $repoRoot $OutputPath }
$directory = Split-Path -Parent $target
if ($directory -ne "" -and -not (Test-Path $directory)) {
    New-Item -ItemType Directory -Force $directory | Out-Null
}
[System.IO.File]::WriteAllText($target, $json, [System.Text.UTF8Encoding]::new($false))

$json
Write-Host "[g6] service verification PASSED"
Write-Host "[g6] report written to $target"
