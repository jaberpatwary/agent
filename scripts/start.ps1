$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$runDir = Join-Path $projectRoot '.run'
$binaryPath = Join-Path $runDir 'autonews-server.exe'
$pidPath = Join-Path $runDir 'server.pid'
$tokenPath = Join-Path $runDir 'shutdown.token'
$portPath = Join-Path $runDir 'server.port'
$stdoutPath = Join-Path $runDir 'server.log'
$stderrPath = Join-Path $runDir 'server-error.log'
$port = if ($env:PORT) { [int]$env:PORT } else { 8080 }
$baseUrl = "http://127.0.0.1:$port"

New-Item -ItemType Directory -Path $runDir -Force | Out-Null

if (Test-Path -LiteralPath $pidPath) {
    $existingPid = 0
    [void][int]::TryParse((Get-Content -LiteralPath $pidPath -Raw).Trim(), [ref]$existingPid)
    $existingProcess = if ($existingPid -gt 0) { Get-CimInstance Win32_Process -Filter "ProcessId=$existingPid" -ErrorAction SilentlyContinue } else { $null }
    if ($existingProcess -and $existingProcess.ExecutablePath -eq $binaryPath) {
        try {
            $health = Invoke-RestMethod -Uri "$baseUrl/health" -TimeoutSec 3
            if ($health.status -eq 'ok') {
                Write-Output "Auto News is already running at $baseUrl"
                exit 0
            }
        } catch { }
        throw "Managed app process $existingPid is running but its health endpoint is unavailable. Run 'make stop' first."
    }
    Remove-Item -LiteralPath $pidPath -Force
    Remove-Item -LiteralPath $tokenPath -Force -ErrorAction SilentlyContinue
}

$databaseUrl = $env:DATABASE_URL
if (-not $databaseUrl) {
    $envFile = Join-Path $projectRoot '.env'
    if (Test-Path -LiteralPath $envFile) {
        $dbLine = Get-Content -LiteralPath $envFile | Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } | Select-Object -First 1
        if ($dbLine) { $databaseUrl = ($dbLine -split '=', 2)[1].Trim().Trim('"', "'") }
    }
}
if (-not $databaseUrl) { throw 'DATABASE_URL is missing. Set it in .env to your local PostgreSQL database.' }

try { $databaseUri = [uri]$databaseUrl } catch { throw 'DATABASE_URL is not a valid PostgreSQL URL.' }
if ($databaseUri.Scheme -ne 'postgres' -and $databaseUri.Scheme -ne 'postgresql') {
    throw 'DATABASE_URL must use the postgres:// or postgresql:// scheme.'
}
if ($databaseUri.Host -notin @('localhost', '127.0.0.1', '::1')) {
    throw 'make start is configured for the local database; DATABASE_URL host must be localhost.'
}
$dbPort = if ($databaseUri.Port -gt 0) { $databaseUri.Port } else { 5432 }
$tcp = [Net.Sockets.TcpClient]::new()
try {
    $connect = $tcp.ConnectAsync($databaseUri.Host, $dbPort)
    if (-not $connect.Wait(3000) -or -not $tcp.Connected) {
        throw "Local PostgreSQL is not reachable at $($databaseUri.Host):$dbPort."
    }
} catch {
    throw "Local PostgreSQL is not reachable at $($databaseUri.Host):$dbPort. Start the PostgreSQL service first."
} finally {
    $tcp.Dispose()
}

Push-Location $projectRoot
try {
    & go build -o $binaryPath ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed; the app was not started.' }
} finally {
    Pop-Location
}

$portCheck = [Net.Sockets.TcpClient]::new()
try {
    $connect = $portCheck.ConnectAsync('127.0.0.1', $port)
    if ($connect.Wait(500) -and $portCheck.Connected) {
        try {
            $health = Invoke-RestMethod -Uri "$baseUrl/health" -TimeoutSec 2
            if ($health.status -eq 'ok') { throw "Another Auto News server is already using port $port. Run 'make stop' if it is the managed server." }
        } catch {
            if ($_.Exception.Message -like 'Another Auto News server*') { throw }
            throw "Port $port is already in use by another process."
        }
    }
} finally {
    $portCheck.Dispose()
}

# Keep a previous Docker run from continuing its RSS and AI workers.
if (Get-Command docker -ErrorAction SilentlyContinue) {
    Push-Location $projectRoot
    try { & docker compose stop app db 2>$null | Out-Null } catch { }
    Pop-Location
}

$randomBytes = [byte[]]::new(32)
$randomGenerator = [Security.Cryptography.RandomNumberGenerator]::Create()
try { $randomGenerator.GetBytes($randomBytes) } finally { $randomGenerator.Dispose() }
$controlToken = [BitConverter]::ToString($randomBytes).Replace('-', '')
[IO.File]::WriteAllText($tokenPath, $controlToken, [Text.UTF8Encoding]::new($false))

$previousDatabaseUrl = $env:DATABASE_URL
$previousControlToken = $env:AUTONEWS_CONTROL_TOKEN
try {
    $env:DATABASE_URL = $databaseUrl
    $env:AUTONEWS_CONTROL_TOKEN = $controlToken
    $process = Start-Process -FilePath $binaryPath -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
} finally {
    $env:DATABASE_URL = $previousDatabaseUrl
    $env:AUTONEWS_CONTROL_TOKEN = $previousControlToken
}

[IO.File]::WriteAllText($pidPath, [string]$process.Id, [Text.UTF8Encoding]::new($false))
[IO.File]::WriteAllText($portPath, [string]$port, [Text.UTF8Encoding]::new($false))

$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    Start-Sleep -Seconds 1
    if ($process.HasExited) { break }
    try {
        $health = Invoke-RestMethod -Uri "$baseUrl/health" -TimeoutSec 2
        if ($health.status -eq 'ok') { $ready = $true; break }
    } catch { }
}

if (-not $ready) {
    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $pidPath, $tokenPath, $portPath -Force -ErrorAction SilentlyContinue
    throw "Auto News did not become healthy. Check .run/server-error.log."
}

Write-Output "Auto News started at $baseUrl (PID $($process.Id))."
Write-Output 'The local PostgreSQL database is in use; RSS and AI workers run only while this app is on.'
