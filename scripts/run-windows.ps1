param(
    [string]$ProviderConfig = ".local\commandcode-provider.json",
    [switch]$CheckOnly
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$python = Join-Path $repoRoot ".venv\Scripts\python.exe"
if (-not (Test-Path $python)) {
    throw 'Python virtual environment not found at .venv. Run: python -m venv .venv; .\.venv\Scripts\python.exe -m pip install -e ".[dev]"'
}

Remove-Item Env:HUD_FIXTURE -ErrorAction SilentlyContinue

$providerPath = Join-Path $repoRoot $ProviderConfig
if (Test-Path $providerPath) {
    $env:HUD_ZCODE_CONFIG = (Resolve-Path $providerPath).Path
    Write-Host "[hud] CommandCode provider mirror: configured (.local)"
} else {
    Remove-Item Env:HUD_ZCODE_CONFIG -ErrorAction SilentlyContinue
    Write-Warning "CommandCode provider mirror not found at $ProviderConfig; CommandCode will stay disabled unless ZCode exposes a readable provider config."
}

Write-Host "[hud] ZCode source: auto-detect"
Write-Host "[hud] Server: http://0.0.0.0:8787"

if ($CheckOnly) {
    Write-Host "[hud] Windows launcher check passed."
    exit 0
}

& $python -m server
exit $LASTEXITCODE
