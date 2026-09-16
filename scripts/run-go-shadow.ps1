param(
    [string]$AgentPath = ".local\bin\ai-control-agent.exe",
    [string]$ProviderConfig = ".local\commandcode-provider.json",
    [string]$Listen = "127.0.0.1:8788",
    [string]$PythonBase = "http://127.0.0.1:8787",
    [switch]$CheckOnly
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$agent = if ([System.IO.Path]::IsPathRooted($AgentPath)) {
    $AgentPath
} else {
    Join-Path $repoRoot $AgentPath
}
if (-not (Test-Path $agent)) {
    throw "Go shadow agent not found at $agent. Copy the CI artifact executable to .local\bin\ai-control-agent.exe or pass -AgentPath with the downloaded EXE path."
}
$agent = (Resolve-Path $agent).Path

Remove-Item Env:AI_CONTROL_FIXTURE -ErrorAction SilentlyContinue
Remove-Item Env:HUD_FIXTURE -ErrorAction SilentlyContinue

$providerPath = if ([System.IO.Path]::IsPathRooted($ProviderConfig)) {
    $ProviderConfig
} else {
    Join-Path $repoRoot $ProviderConfig
}
if (Test-Path $providerPath) {
    $env:HUD_ZCODE_CONFIG = (Resolve-Path $providerPath).Path
    Write-Host "[shadow] CommandCode provider mirror: configured (.local)"
} else {
    Remove-Item Env:HUD_ZCODE_CONFIG -ErrorAction SilentlyContinue
    Write-Warning "CommandCode provider mirror not found at $ProviderConfig; Go CommandCode source may stay disabled."
}

$pythonHealth = $PythonBase.TrimEnd('/') + "/api/v1/health"
try {
    Invoke-RestMethod -Uri $pythonHealth -Method Get -TimeoutSec 2 | Out-Null
    Write-Host "[shadow] Python reference: reachable at $PythonBase"
} catch {
    Write-Warning "Python reference is NOT running at $PythonBase. Start .\scripts\run-windows.ps1 in another terminal before running compare-shadow.ps1. Go can still run independently on 8788."
}

Write-Host "[shadow] ZCode source: auto-detect"
Write-Host "[shadow] Go candidate: http://$Listen"

if ($CheckOnly) {
    Write-Host "[shadow] launcher check passed."
    exit 0
}

& $agent run -listen $Listen
exit $LASTEXITCODE
