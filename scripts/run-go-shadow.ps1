param(
    [string]$AgentPath = ".local\bin\ai-control-agent.exe",
    [string]$ProviderConfig = ".local\commandcode-provider.json",
    [string]$Listen = "127.0.0.1:8788",
    [switch]$CheckOnly
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$agent = Join-Path $repoRoot $AgentPath
if (-not (Test-Path $agent)) {
    throw "Go shadow agent not found at $AgentPath. Place the CI artifact executable there first."
}

Remove-Item Env:AI_CONTROL_FIXTURE -ErrorAction SilentlyContinue
Remove-Item Env:HUD_FIXTURE -ErrorAction SilentlyContinue

$providerPath = Join-Path $repoRoot $ProviderConfig
if (Test-Path $providerPath) {
    $env:HUD_ZCODE_CONFIG = (Resolve-Path $providerPath).Path
    Write-Host "[shadow] CommandCode provider mirror: configured (.local)"
} else {
    Remove-Item Env:HUD_ZCODE_CONFIG -ErrorAction SilentlyContinue
    Write-Warning "CommandCode provider mirror not found at $ProviderConfig; Go CommandCode source may stay disabled."
}

Write-Host "[shadow] ZCode source: auto-detect"
Write-Host "[shadow] Go candidate: http://$Listen"
Write-Host "[shadow] Python reference remains unchanged on http://127.0.0.1:8787"

if ($CheckOnly) {
    Write-Host "[shadow] launcher check passed."
    exit 0
}

& $agent run -listen $Listen
exit $LASTEXITCODE
