param(
    [string]$AgentPath = ".local\bin\ai-control-agent.exe",
    [string]$ProviderConfig = ".local\commandcode-provider.json",
    [string]$Listen = "0.0.0.0:8787",
    [switch]$CheckOnly
)

$ErrorActionPreference = "Stop"

function Test-TcpPort([string]$HostName, [int]$Port, [int]$TimeoutMs = 500) {
    $client = [System.Net.Sockets.TcpClient]::new()
    try {
        $pending = $client.BeginConnect($HostName, $Port, $null, $null)
        if (-not $pending.AsyncWaitHandle.WaitOne($TimeoutMs)) {
            return $false
        }
        $client.EndConnect($pending)
        return $true
    }
    catch {
        return $false
    }
    finally {
        $client.Dispose()
    }
}

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$agent = if ([System.IO.Path]::IsPathRooted($AgentPath)) {
    $AgentPath
} else {
    Join-Path $repoRoot $AgentPath
}
if (-not (Test-Path $agent)) {
    throw "Go agent not found at $agent. Copy the validated CI artifact to .local\bin\ai-control-agent.exe or pass -AgentPath."
}
$agent = (Resolve-Path $agent).Path

$listenParts = $Listen.Split(':')
if ($listenParts.Count -ne 2) {
    throw "Unsupported listen address '$Listen'. Expected host:port."
}
$listenPort = [int]$listenParts[1]
$portBusy = Test-TcpPort "127.0.0.1" $listenPort
if ($portBusy -and -not $CheckOnly) {
    throw "TCP port $listenPort is already in use. Stop the Python 8787 reference (or any other listener) before starting the Go production agent."
}

Remove-Item Env:AI_CONTROL_FIXTURE -ErrorAction SilentlyContinue
Remove-Item Env:HUD_FIXTURE -ErrorAction SilentlyContinue
Remove-Item Env:HUD_COMMANDCODE_PROVIDER_ID -ErrorAction SilentlyContinue

$providerPath = if ([System.IO.Path]::IsPathRooted($ProviderConfig)) {
    $ProviderConfig
} else {
    Join-Path $repoRoot $ProviderConfig
}
if (Test-Path $providerPath) {
    $env:HUD_ZCODE_CONFIG = (Resolve-Path $providerPath).Path
    Write-Host "[go] CommandCode provider mirror: configured (.local)"
} else {
    Remove-Item Env:HUD_ZCODE_CONFIG -ErrorAction SilentlyContinue
    Write-Warning "CommandCode provider mirror not found at $ProviderConfig; CommandCode may stay disabled."
}

Write-Host "[go] ZCode source: auto-detect"
Write-Host "[go] Production listener: http://$Listen"
Write-Host "[go] Android backend URL stays unchanged."
if ($portBusy) {
    Write-Warning "[go] Port $listenPort is currently occupied; stop that process before actual cutover."
} else {
    Write-Host "[go] Port $listenPort is free."
}

if ($CheckOnly) {
    Write-Host "[go] production launcher check passed."
    exit 0
}

Write-Host "[go] If Windows Firewall prompts, allow this executable on Private networks so the Android device can reach it."
& $agent run -listen $Listen
exit $LASTEXITCODE
