param(
    [string]$Agent = ".\agent\ai-control-agent.exe",
    [int]$Requests = 1500
)

$ErrorActionPreference = "Stop"

if ($Requests -lt 300 -or $Requests -gt 10000) {
    throw "Requests must be within 300..10000"
}

$repoRoot = Split-Path -Parent $PSScriptRoot
$agentPath = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $Agent))
$fixture = Join-Path $repoRoot "server\tests\fixtures\healthy.json"
if (-not (Test-Path $agentPath)) { throw "Agent binary not found: $agentPath" }
if (-not (Test-Path $fixture)) { throw "fixture not found: $fixture" }

$port = 18891
$baseUrl = "http://127.0.0.1:$port"
$stdout = Join-Path $env:RUNNER_TEMP "ai-control-agent-resource.stdout.log"
$stderr = Join-Path $env:RUNNER_TEMP "ai-control-agent-resource.stderr.log"
Remove-Item $stdout, $stderr -Force -ErrorAction SilentlyContinue

function Get-Sample([int]$ProcessId) {
    $p = Get-Process -Id $ProcessId -ErrorAction Stop
    $p.Refresh()
    return [pscustomobject]@{
        WorkingSet = [int64]$p.WorkingSet64
        PrivateBytes = [int64]$p.PrivateMemorySize64
        Handles = [int]$p.HandleCount
        Threads = [int]$p.Threads.Count
        CpuSeconds = [double]$p.CPU
    }
}

function Wait-Ready([System.Net.Http.HttpClient]$Client) {
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    $last = $null
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $body = $Client.GetStringAsync("$baseUrl/api/v1/health").GetAwaiter().GetResult()
            $health = $body | ConvertFrom-Json
            if ($health.schemaVersion -eq 1 -and $health.status -eq "ok") { return }
        } catch {
            $last = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Agent resource smoke readiness timed out: $last"
}

$process = $null
$client = $null
try {
    $process = Start-Process -FilePath $agentPath `
        -ArgumentList @("run", "--fixture", $fixture, "--listen", "127.0.0.1:$port") `
        -PassThru `
        -RedirectStandardOutput $stdout `
        -RedirectStandardError $stderr

    $handler = [System.Net.Http.HttpClientHandler]::new()
    $client = [System.Net.Http.HttpClient]::new($handler)
    $client.Timeout = [TimeSpan]::FromSeconds(3)
    Wait-Ready $client

    # Warm the Go HTTP/JSON paths before taking the baseline sample.
    for ($i = 0; $i -lt 100; $i++) {
        $null = $client.GetStringAsync("$baseUrl/api/v1/state").GetAwaiter().GetResult()
        $null = $client.GetStringAsync("$baseUrl/api/v1/diagnostics").GetAwaiter().GetResult()
    }
    Start-Sleep -Milliseconds 300
    $before = Get-Sample $process.Id

    $idleStart = Get-Sample $process.Id
    Start-Sleep -Seconds 2
    $idleEnd = Get-Sample $process.Id
    $idleCpu = $idleEnd.CpuSeconds - $idleStart.CpuSeconds
    if ($idleCpu -gt 0.20) {
        throw ("Agent idle CPU time too high over 2s: {0:N3}s" -f $idleCpu)
    }

    $paths = @("/api/v1/state", "/api/v1/health", "/api/v1/diagnostics")
    for ($i = 0; $i -lt $Requests; $i++) {
        $path = $paths[$i % $paths.Count]
        $null = $client.GetStringAsync($baseUrl + $path).GetAwaiter().GetResult()
    }

    Start-Sleep -Milliseconds 750
    $after = Get-Sample $process.Id

    $workingGrowth = $after.WorkingSet - $before.WorkingSet
    $privateGrowth = $after.PrivateBytes - $before.PrivateBytes
    $handleGrowth = $after.Handles - $before.Handles
    $threadGrowth = $after.Threads - $before.Threads

    if ($handleGrowth -gt 8) { throw "Agent handle growth too high: $handleGrowth" }
    if ($threadGrowth -gt 4) { throw "Agent thread growth too high: $threadGrowth" }
    if ($workingGrowth -gt 64MB) { throw "Agent Working Set growth exceeds 64 MiB gross leak guard: $workingGrowth" }
    if ($privateGrowth -gt 64MB) { throw "Agent Private Bytes growth exceeds 64 MiB gross leak guard: $privateGrowth" }

    Write-Host ("[agent-resource-smoke] PASSED requests={0} ws_before_mib={1:N1} ws_after_mib={2:N1} private_before_mib={3:N1} private_after_mib={4:N1} handles={5}->{6} threads={7}->{8} idle_cpu_2s={9:N3}s" -f `
        $Requests, ($before.WorkingSet / 1MB), ($after.WorkingSet / 1MB), `
        ($before.PrivateBytes / 1MB), ($after.PrivateBytes / 1MB), `
        $before.Handles, $after.Handles, $before.Threads, $after.Threads, $idleCpu)
} finally {
    if ($null -ne $client) { $client.Dispose() }
    if ($null -ne $process) {
        try {
            if (-not $process.HasExited) { Stop-Process -Id $process.Id -Force }
        } catch {}
        try { $process.WaitForExit(5000) | Out-Null } catch {}
    }
    if ($null -ne $process -and $process.ExitCode -notin @(0, -1) -and (Test-Path $stderr)) {
        Write-Host (Get-Content $stderr -Raw)
    }
}
