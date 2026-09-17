param(
    [Parameter(Mandatory = $true)]
    [string]$Exe,
    [Parameter(Mandatory = $false)]
    [string]$MetricsPath = ""
)

$ErrorActionPreference = "Stop"

Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class HudNativeMetrics {
    [DllImport("user32.dll")]
    public static extern bool ShowWindowAsync(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")]
    public static extern int GetGuiResources(IntPtr hProcess, int uiFlags);
}
"@

$SW_HIDE = 0
$SW_SHOW = 5
$GR_GDIOBJECTS = 0
$GR_USEROBJECTS = 1
$WarmupSeconds = 3
$CpuSampleSeconds = 4
$ToggleCycles = 24
$WorkingSetTargetMiB = 48
$MaxMemoryGrowthMiB = 16
$MaxHandleGrowth = 16
$MaxThreadGrowth = 4
$MaxGuiGrowth = 4
$MaxVisibleCpuSeconds = 0.35
$MaxHiddenCpuSeconds = 0.12

function Snapshot-UiProcess([System.Diagnostics.Process]$Process) {
    $Process.Refresh()
    return [ordered]@{
        workingSet = [int64]$Process.WorkingSet64
        privateBytes = [int64]$Process.PrivateMemorySize64
        handles = [int]$Process.HandleCount
        threads = [int]$Process.Threads.Count
        gdi = [int][HudNativeMetrics]::GetGuiResources($Process.Handle, $GR_GDIOBJECTS)
        user = [int][HudNativeMetrics]::GetGuiResources($Process.Handle, $GR_USEROBJECTS)
        cpuSeconds = [double]$Process.TotalProcessorTime.TotalSeconds
    }
}

function Wait-MainWindow([System.Diagnostics.Process]$Process) {
    for ($i = 0; $i -lt 50; $i++) {
        if ($Process.HasExited) {
            throw "Windows UI exited before creating its main window (code $($Process.ExitCode))"
        }
        $Process.Refresh()
        if ($Process.MainWindowHandle -ne [IntPtr]::Zero) {
            return $Process.MainWindowHandle
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Windows UI did not expose a main window within 5 seconds"
}

function Delta-MiB([int64]$After, [int64]$Before) {
    return [Math]::Round(($After - $Before) / 1MB, 2)
}

if (-not (Test-Path -LiteralPath $Exe)) {
    throw "Windows UI executable not found: $Exe"
}

$process = Start-Process -FilePath $Exe -PassThru
try {
    $window = Wait-MainWindow $process
    Start-Sleep -Seconds $WarmupSeconds
    $baseline = Snapshot-UiProcess $process

    $workingSetMiB = [Math]::Round($baseline.workingSet / 1MB, 2)
    $privateMiB = [Math]::Round($baseline.privateBytes / 1MB, 2)
    if ($workingSetMiB -gt $WorkingSetTargetMiB) {
        Write-Warning "Visible working set ${workingSetMiB} MiB exceeds the soft ${WorkingSetTargetMiB} MiB target"
    }

    $visibleCpuStart = $baseline.cpuSeconds
    Start-Sleep -Seconds $CpuSampleSeconds
    $visibleSample = Snapshot-UiProcess $process
    $visibleCpu = [Math]::Round($visibleSample.cpuSeconds - $visibleCpuStart, 4)
    if ($visibleCpu -gt $MaxVisibleCpuSeconds) {
        throw "Visible CPU time ${visibleCpu}s over ${CpuSampleSeconds}s exceeds hard smoke limit ${MaxVisibleCpuSeconds}s"
    }

    for ($i = 0; $i -lt $ToggleCycles; $i++) {
        [void][HudNativeMetrics]::ShowWindowAsync($window, $SW_HIDE)
        Start-Sleep -Milliseconds 35
        [void][HudNativeMetrics]::ShowWindowAsync($window, $SW_SHOW)
        Start-Sleep -Milliseconds 35
    }
    Start-Sleep -Milliseconds 500
    $afterCycles = Snapshot-UiProcess $process

    $workingSetGrowthMiB = Delta-MiB $afterCycles.workingSet $baseline.workingSet
    $privateGrowthMiB = Delta-MiB $afterCycles.privateBytes $baseline.privateBytes
    $handleGrowth = $afterCycles.handles - $baseline.handles
    $threadGrowth = $afterCycles.threads - $baseline.threads
    $gdiGrowth = $afterCycles.gdi - $baseline.gdi
    $userGrowth = $afterCycles.user - $baseline.user

    if ($workingSetGrowthMiB -gt $MaxMemoryGrowthMiB) { throw "Working-set growth ${workingSetGrowthMiB} MiB exceeds ${MaxMemoryGrowthMiB} MiB" }
    if ($privateGrowthMiB -gt $MaxMemoryGrowthMiB) { throw "Private-byte growth ${privateGrowthMiB} MiB exceeds ${MaxMemoryGrowthMiB} MiB" }
    if ($handleGrowth -gt $MaxHandleGrowth) { throw "Handle growth $handleGrowth exceeds $MaxHandleGrowth" }
    if ($threadGrowth -gt $MaxThreadGrowth) { throw "Thread growth $threadGrowth exceeds $MaxThreadGrowth" }
    if ($gdiGrowth -gt $MaxGuiGrowth) { throw "GDI-object growth $gdiGrowth exceeds $MaxGuiGrowth" }
    if ($userGrowth -gt $MaxGuiGrowth) { throw "USER-object growth $userGrowth exceeds $MaxGuiGrowth" }

    [void][HudNativeMetrics]::ShowWindowAsync($window, $SW_HIDE)
    Start-Sleep -Milliseconds 250
    $hiddenStart = Snapshot-UiProcess $process
    Start-Sleep -Seconds $CpuSampleSeconds
    $hiddenEnd = Snapshot-UiProcess $process
    $hiddenCpu = [Math]::Round($hiddenEnd.cpuSeconds - $hiddenStart.cpuSeconds, 4)
    if ($hiddenCpu -gt $MaxHiddenCpuSeconds) {
        throw "Hidden CPU time ${hiddenCpu}s over ${CpuSampleSeconds}s exceeds hard smoke limit ${MaxHiddenCpuSeconds}s"
    }

    $metrics = [ordered]@{
        schemaVersion = 1
        workingSetMiB = $workingSetMiB
        privateMiB = $privateMiB
        visibleCpuSecondsOver4s = $visibleCpu
        hiddenCpuSecondsOver4s = $hiddenCpu
        toggleCycles = $ToggleCycles
        growth = [ordered]@{
            workingSetMiB = $workingSetGrowthMiB
            privateMiB = $privateGrowthMiB
            handles = $handleGrowth
            threads = $threadGrowth
            gdi = $gdiGrowth
            user = $userGrowth
        }
        baseline = $baseline
        afterCycles = $afterCycles
    }

    $json = $metrics | ConvertTo-Json -Depth 5
    Write-Host "[windows-ui-resource-smoke] PASS"
    Write-Host $json
    if ($MetricsPath -ne "") {
        $directory = Split-Path -Parent $MetricsPath
        if ($directory -ne "") { New-Item -ItemType Directory -Force -Path $directory | Out-Null }
        Set-Content -LiteralPath $MetricsPath -Value $json -Encoding utf8
    }
}
finally {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        $process.WaitForExit(5000) | Out-Null
    }
}
