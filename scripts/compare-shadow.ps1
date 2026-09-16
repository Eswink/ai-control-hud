param(
    [string]$PythonBase = "http://127.0.0.1:8787",
    [string]$GoBase = "http://127.0.0.1:8788",
    [string]$OutputPath = ""
)

$ErrorActionPreference = "Stop"

function Get-State([string]$Base) {
    Invoke-RestMethod -Uri ($Base.TrimEnd('/') + "/api/v1/state") -Method Get -TimeoutSec 8
}

function First-Task($State) {
    if ($null -eq $State.zcode.tasks -or $State.zcode.tasks.Count -eq 0) {
        return $null
    }
    return $State.zcode.tasks[0]
}

function Window-ByName($State, [string]$Name) {
    if ($null -eq $State.commandCode.usage -or $null -eq $State.commandCode.usage.windows) {
        return $null
    }
    return $State.commandCode.usage.windows | Where-Object { $_.name -eq $Name } | Select-Object -First 1
}

$python = Get-State $PythonBase
$go = Get-State $GoBase

$pTask = First-Task $python
$gTask = First-Task $go
$p5 = Window-ByName $python "5h"
$g5 = Window-ByName $go "5h"
$pWeek = Window-ByName $python "weekly"
$gWeek = Window-ByName $go "weekly"

function Numeric-Delta($A, $B) {
    if ($null -eq $A -or $null -eq $B) { return $null }
    return [math]::Abs(([double]$A) - ([double]$B))
}

$report = [ordered]@{
    generatedAt = (Get-Date).ToUniversalTime().ToString("o")
    endpoints = [ordered]@{
        python = $PythonBase
        go = $GoBase
    }
    exactChecks = [ordered]@{
        schemaVersion = ($python.schemaVersion -eq $go.schemaVersion)
        overallStatus = ($python.overall.status -eq $go.overall.status)
        zcodeHealth = ($python.zcode.health.status -eq $go.zcode.health.status)
        zcodeRunning = ($python.zcode.summary.running -eq $go.zcode.summary.running)
        zcodeWaiting = ($python.zcode.summary.waiting -eq $go.zcode.summary.waiting)
        zcodeFailed = ($python.zcode.summary.failed -eq $go.zcode.summary.failed)
        zcodeCompleted = ($python.zcode.summary.completed -eq $go.zcode.summary.completed)
        taskTitle = (($null -eq $pTask -and $null -eq $gTask) -or ($null -ne $pTask -and $null -ne $gTask -and $pTask.title -eq $gTask.title))
        taskWorkspace = (($null -eq $pTask -and $null -eq $gTask) -or ($null -ne $pTask -and $null -ne $gTask -and $pTask.workspace -eq $gTask.workspace))
        taskStatus = (($null -eq $pTask -and $null -eq $gTask) -or ($null -ne $pTask -and $null -ne $gTask -and $pTask.status -eq $gTask.status))
        taskActivity = (($null -eq $pTask -and $null -eq $gTask) -or ($null -ne $pTask -and $null -ne $gTask -and $pTask.activity -eq $gTask.activity))
        commandCodeHealth = ($python.commandCode.health.status -eq $go.commandCode.health.status)
        plan = ($python.commandCode.usage.plan -eq $go.commandCode.usage.plan)
        creditLimit = ($python.commandCode.usage.credit.limit -eq $go.commandCode.usage.credit.limit)
        creditUnit = ($python.commandCode.usage.credit.unit -eq $go.commandCode.usage.credit.unit)
    }
    numericDeltas = [ordered]@{
        creditRemaining = Numeric-Delta $python.commandCode.usage.credit.remaining $go.commandCode.usage.credit.remaining
        fiveHourUsedPercent = Numeric-Delta $p5.usedPercent $g5.usedPercent
        weeklyUsedPercent = Numeric-Delta $pWeek.usedPercent $gWeek.usedPercent
    }
    snapshots = [ordered]@{
        python = [ordered]@{
            overall = $python.overall.status
            zcode = [ordered]@{
                health = $python.zcode.health.status
                summary = $python.zcode.summary
                firstTask = $pTask
            }
            commandCode = [ordered]@{
                health = $python.commandCode.health.status
                plan = $python.commandCode.usage.plan
                credit = $python.commandCode.usage.credit
                fiveHour = $p5
                weekly = $pWeek
            }
        }
        go = [ordered]@{
            overall = $go.overall.status
            zcode = [ordered]@{
                health = $go.zcode.health.status
                summary = $go.zcode.summary
                firstTask = $gTask
            }
            commandCode = [ordered]@{
                health = $go.commandCode.health.status
                plan = $go.commandCode.usage.plan
                credit = $go.commandCode.usage.credit
                fiveHour = $g5
                weekly = $gWeek
            }
        }
    }
}

$json = $report | ConvertTo-Json -Depth 12
$json

if ($OutputPath -ne "") {
    $repoRoot = Split-Path -Parent $PSScriptRoot
    $target = if ([System.IO.Path]::IsPathRooted($OutputPath)) { $OutputPath } else { Join-Path $repoRoot $OutputPath }
    $directory = Split-Path -Parent $target
    if ($directory -ne "" -and -not (Test-Path $directory)) {
        New-Item -ItemType Directory -Force $directory | Out-Null
    }
    [System.IO.File]::WriteAllText($target, $json, [System.Text.UTF8Encoding]::new($false))
    Write-Host "[shadow] comparison written to $target"
}
