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
    [DllImport("user32.dll", SetLastError=true)]
    public static extern bool PostMessageW(IntPtr hWnd, uint msg, UIntPtr wParam, IntPtr lParam);
}
"@

$SW_HIDE = 0
$SW_SHOW = 5
$GR_GDIOBJECTS = 0
$GR_USEROBJECTS = 1
$WM_KEYDOWN = 0x0100
$VK_M = 0x4D
$WarmupSeconds = 3
$CpuSampleSeconds = 4
$ShowHideCycles = 16
$MiniHudToggleCycles = 20
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

function Assert-Growth([System.Collections.IDictionary]$After, [System.Collections.IDictionary]$Before, [string]$Phase) {
    $workingSetGrowthMiB = Delta-MiB $After.workingSet $Before.workingSet
    $privateGrowthMiB = Delta-MiB $After.privateBytes $Before.privateBytes
    $handleGrowth = $After.handles - $Before.handles
    $threadGrowth = $After.threads - $Before.threads
    $gdiGrowth = $After.gdi - $Before.gdi
    $userGrowth = $After.user - $Before.user

    if ($workingSetGrowthMiB -gt $MaxMemoryGrowthMiB) { throw "$Phase working-set growth ${workingSetGrowthMiB} MiB exceeds ${MaxMemoryGrowthMiB} MiB" }
    if ($privateGrowthMiB -gt $MaxMemoryGrowthMiB) { throw "$Phase private-byte growth ${privateGrowthMiB} MiB exceeds ${MaxMemoryGrowthMiB} MiB" }
    if ($handleGrowth -gt $MaxHandleGrowth) { throw "$Phase handle growth $handleGrowth exceeds $MaxHandleGrowth" }
    if ($threadGrowth -gt $MaxThreadGrowth) { throw "$Phase thread growth $threadGrowth exceeds $MaxThreadGrowth" }
    if ($gdiGrowth -gt $MaxGuiGrowth) { throw "$Phase GDI-object growth $gdiGrowth exceeds $MaxGuiGrowth" }
    if ($userGrowth -gt $MaxGuiGrowth) { throw "$Phase USER-object growth $userGrowth exceeds $MaxGuiGrowth" }

    return [ordered]@{
        workingSetMiB = $workingSetGrowthMiB
        privateMiB = $privateGrowthMiB
        handles = $handleGrowth
        threads = $threadGrowth
        gdi = $gdiGrowth
        user = $userGrowth
    }
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

    # Exercise the real UI7 Mini HUD transition path. Each pair enters and exits
    # Mini HUD, forcing D2D target recreation, topmost changes and state writes.
    for ($i = 0; $i -lt $MiniHudToggleCycles; $i++) {
        if (-not [HudNativeMetrics]::PostMessageW($window, $WM_KEYDOWN, [UIntPtr]$VK_M, [IntPtr]::Zero)) {
            throw "Unable to post Mini HUD toggle at cycle $i"
        }
        Start-Sleep -Milliseconds 45
        if (-not [HudNativeMetrics]::PostMessageW($window, $WM_KEYDOWN, [UIntPtr]$VK_M, [IntPtr]::Zero)) {
            throw "Unable to post full-dashboard toggle at cycle $i"
        }
        Start-Sleep -Milliseconds 45
    }
    Start-Sleep -Milliseconds 600
    $afterMiniHud = Snapshot-UiProcess $process
    $miniHudGrowth = Assert-Growth $afterMiniHud $baseline "Mini HUD"

    for ($i = 0; $i -lt $ShowHideCycles; $i++) {
        [void][HudNativeMetrics]::ShowWindowAsync($window, $SW_HIDE)
        Start-Sleep -Milliseconds 35
        [void][HudNativeMetrics]::ShowWindowAsync($window, $SW_SHOW)
        Start-Sleep -Milliseconds 35
    }
    Start-Sleep -Milliseconds 500
    $afterCycles = Snapshot-UiProcess $process
    $showHideGrowth = Assert-Growth $afterCycles $baseline "Show/hide"

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
        schemaVersion = 2
        workingSetMiB = $workingSetMiB
        privateMiB = $privateMiB
        visibleCpuSecondsOver4s = $visibleCpu
        hiddenCpuSecondsOver4s = $hiddenCpu
        miniHudToggleCycles = $MiniHudToggleCycles
        showHideCycles = $ShowHideCycles
        growth = [ordered]@{
            miniHud = $miniHudGrowth
            showHide = $showHideGrowth
        }
        baseline = $baseline
        afterMiniHud = $afterMiniHud
        afterCycles = $afterCycles
    }

    $json = $metrics | ConvertTo-Json -Depth 6
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
