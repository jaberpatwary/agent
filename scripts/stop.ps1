$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$runDir = Join-Path $projectRoot '.run'
$pidPath = Join-Path $runDir 'server.pid'
$tokenPath = Join-Path $runDir 'shutdown.token'
$portPath = Join-Path $runDir 'server.port'
$port = 8080
if (Test-Path -LiteralPath $portPath) {
    $parsedPort = 0
    if ([int]::TryParse((Get-Content -LiteralPath $portPath -Raw).Trim(), [ref]$parsedPort)) { $port = $parsedPort }
}
$stoppedLocal = $false

if ((Test-Path -LiteralPath $pidPath) -and (Test-Path -LiteralPath $tokenPath)) {
    $appPid = 0
    [void][int]::TryParse((Get-Content -LiteralPath $pidPath -Raw).Trim(), [ref]$appPid)
    $appProcess = if ($appPid -gt 0) { Get-CimInstance Win32_Process -Filter "ProcessId=$appPid" -ErrorAction SilentlyContinue } else { $null }
    $expectedBinary = Join-Path $runDir 'autonews-server.exe'

    if ($appProcess -and $appProcess.ExecutablePath -eq $expectedBinary) {
        $controlToken = (Get-Content -LiteralPath $tokenPath -Raw).Trim()
        try {
            Invoke-RestMethod -Uri "http://127.0.0.1:$port/_control/shutdown" -Method Post `
                -Headers @{ 'X-Autonews-Control' = $controlToken } -TimeoutSec 5 | Out-Null
        } catch { }

        try { Wait-Process -Id $appPid -Timeout 35 -ErrorAction Stop } catch { }
        $appProcess = Get-CimInstance Win32_Process -Filter "ProcessId=$appPid" -ErrorAction SilentlyContinue
        if ($appProcess -and $appProcess.ExecutablePath -eq $expectedBinary) {
            Stop-Process -Id $appPid -Force -ErrorAction SilentlyContinue
        }
        $stoppedLocal = $true
    }

    Remove-Item -LiteralPath $pidPath, $tokenPath, $portPath -Force -ErrorAction SilentlyContinue
}

# Stop any older Compose instance too, so no container worker keeps calling AI.
if (Get-Command docker -ErrorAction SilentlyContinue) {
    Push-Location $projectRoot
    try { & docker compose stop app db 2>$null | Out-Null } catch { }
    Pop-Location
}

if ($stoppedLocal) {
    Write-Output 'Auto News is stopped. Its RSS and AI workers are no longer running.'
} else {
    Write-Output 'Auto News was already stopped. Its RSS and AI workers are not running.'
}
