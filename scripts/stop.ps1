# make stop — stop every container of this project so no AI tokens are used.
# Data (database, photos, n8n) is kept in Docker volumes; "make start" resumes.
# 'Continue': docker prints progress and warnings on stderr, which Windows
# PowerShell 5.1 would otherwise turn into terminating errors. Failures are
# detected from $LASTEXITCODE instead.
$ErrorActionPreference = 'Continue'

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

& docker info 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) {
    Write-Output 'Docker is not running, so nothing from this project is running either. No AI tokens are being used.'
} else {
    # n8n first: it is the scheduler that wakes the AI agent.
    Write-Output 'Stopping n8n (the agent scheduler)...'
    & docker compose stop n8n
    Write-Output 'Stopping the app and database...'
    & docker compose stop app db
    if ($LASTEXITCODE -ne 0) { throw 'docker compose stop failed.' }

    $running = & docker compose ps --status running --services
    if ($running) {
        throw ("Still running: {0}" -f ($running -join ', '))
    }
    Write-Output 'All project containers are stopped. The AI agent cannot run, so no API tokens are used.'
}

# Optional: also suspend the hosted copy on Render, if a key is configured.
$envFile = Join-Path $projectRoot '.env'
$renderKey = $env:RENDER_API_KEY
if (-not $renderKey -and (Test-Path -LiteralPath $envFile)) {
    $line = Get-Content -LiteralPath $envFile | Where-Object { $_ -match '^\s*RENDER_API_KEY\s*=\s*\S' } | Select-Object -First 1
    if ($line) { $renderKey = 'set' }
}
if ($renderKey) {
    try {
        & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'render-control.ps1') -Action suspend
    } catch {
        Write-Output ("Render suspend skipped: {0}" -f $_.Exception.Message)
    }
}
