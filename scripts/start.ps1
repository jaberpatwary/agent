# make start — bring up the whole project (Postgres, app, n8n) with Docker Compose.
# AI tokens are only spent while it runs: n8n wakes the agent every 15 minutes.
# 'Continue': docker prints progress and warnings on stderr, which Windows
# PowerShell 5.1 would otherwise turn into terminating errors. Failures are
# detected from $LASTEXITCODE instead.
$ErrorActionPreference = 'Continue'

$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location $projectRoot

if (-not (Test-Path -LiteralPath (Join-Path $projectRoot '.env'))) {
    throw '.env is missing. Copy .env.example to .env and add your API keys first.'
}

function Test-Docker {
    & docker info 2>&1 | Out-Null
    return ($LASTEXITCODE -eq 0)
}

if (-not (Test-Docker)) {
    $desktop = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
    if (-not (Test-Path -LiteralPath $desktop)) {
        throw 'Docker is not running and Docker Desktop was not found. Start Docker first.'
    }
    Write-Output 'Docker Desktop is not running - starting it (this can take a minute)...'
    Start-Process -FilePath $desktop | Out-Null
    $deadline = (Get-Date).AddMinutes(3)
    while (-not (Test-Docker)) {
        if ((Get-Date) -gt $deadline) { throw 'Docker did not become ready within 3 minutes.' }
        Start-Sleep -Seconds 3
    }
}

Write-Output 'Starting database, app and n8n (rebuilding the app if the code changed)...'
& docker compose up -d --build
if ($LASTEXITCODE -ne 0) { throw 'docker compose up failed.' }

function Wait-Url([string] $Url, [string] $Name) {
    $deadline = (Get-Date).AddSeconds(120)
    while ((Get-Date) -lt $deadline) {
        try {
            $r = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 3
            if ($r.StatusCode -eq 200) { Write-Output ("  {0} is up" -f $Name); return }
        } catch { }
        Start-Sleep -Seconds 2
    }
    throw ("{0} did not become healthy. Check: docker compose logs" -f $Name)
}
Wait-Url 'http://localhost:8081/health' 'App'
Wait-Url 'http://localhost:5678/healthz' 'n8n'

Write-Output ''
Write-Output 'Bangla News is running:'
Write-Output '  Website      http://localhost:8081'
Write-Output '  Admin        http://localhost:8081/admin/login'
Write-Output '  Agent desk   http://localhost:8081/admin/agent'
Write-Output '  n8n          http://localhost:5678'
Write-Output ''
Write-Output 'The AI agent now runs every 15 minutes and uses API tokens. Run "make stop" to stop all AI usage.'
