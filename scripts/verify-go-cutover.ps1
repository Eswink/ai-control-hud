param(
    [string]$Base = "http://127.0.0.1:8787",
    [string]$OutputPath = ".local\g5-cutover.json"
)

$ErrorActionPreference = "Stop"

$state = Invoke-RestMethod -Uri ($Base.TrimEnd('/') + "/api/v1/state") -Method Get -TimeoutSec 8

$checks = [ordered]@{
    schemaVersion = ($state.schemaVersion -eq 1)
    goServer = ($state.server.version -like "*go*")
    overallLive = ($state.overall.status -eq "live")
    zcodeOK = ($state.zcode.health.status -eq "ok")
    commandCodeOK = ($state.commandCode.health.status -eq "ok")
}

$passed = -not ($checks.Values -contains $false)
$report = [ordered]@{
    generatedAt = (Get-Date).ToUniversalTime().ToString("o")
    endpoint = $Base
    result = if ($passed) { "passed" } else { "failed" }
    checks = $checks
    server = [ordered]@{
        version = $state.server.version
        uptimeSeconds = $state.server.uptimeSeconds
    }
    zcode = [ordered]@{
        health = $state.zcode.health.status
        summary = $state.zcode.summary
        firstTask = if ($null -ne $state.zcode.tasks -and $state.zcode.tasks.Count -gt 0) { $state.zcode.tasks[0] } else { $null }
    }
    commandCode = [ordered]@{
        health = $state.commandCode.health.status
        usage = $state.commandCode.usage
    }
    androidVerificationRequired = $true
}

$json = $report | ConvertTo-Json -Depth 12
$json

$repoRoot = Split-Path -Parent $PSScriptRoot
$target = if ([System.IO.Path]::IsPathRooted($OutputPath)) { $OutputPath } else { Join-Path $repoRoot $OutputPath }
$directory = Split-Path -Parent $target
if ($directory -ne "" -and -not (Test-Path $directory)) {
    New-Item -ItemType Directory -Force $directory | Out-Null
}
[System.IO.File]::WriteAllText($target, $json, [System.Text.UTF8Encoding]::new($false))
Write-Host "[g5] verification written to $target"

if (-not $passed) {
    throw "G5 local Go cutover verification failed. Review the checks above before testing Android."
}

Write-Host "[g5] local Go cutover checks passed. Verify the Android device reconnects without changing its configured URL."
