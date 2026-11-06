[CmdletBinding()]
param (
    [Parameter(Mandatory = $true)]
    [ValidateSet('suspend', 'resume')]
    [string] $Action
)

$ErrorActionPreference = 'Stop'

function Get-DotEnvValue {
    param (
        [Parameter(Mandatory = $true)] [string] $Name,
        [Parameter(Mandatory = $true)] [string] $Path
    )

    if (-not (Test-Path -LiteralPath $Path)) { return $null }

    $pattern = '^\s*' + [regex]::Escape($Name) + '\s*=\s*(.*?)\s*$'
    foreach ($line in Get-Content -LiteralPath $Path) {
        $match = [regex]::Match($line, $pattern)
        if (-not $match.Success) { continue }

        $value = $match.Groups[1].Value.Trim()
        if ($value.Length -ge 2 -and
            (($value.StartsWith('"') -and $value.EndsWith('"')) -or
             ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        return $value
    }

    return $null
}

$projectRoot = Split-Path -Parent $PSScriptRoot
$envFile = Join-Path $projectRoot '.env'
$apiKey = $env:RENDER_API_KEY
if (-not $apiKey) { $apiKey = Get-DotEnvValue -Name 'RENDER_API_KEY' -Path $envFile }
if (-not $apiKey) {
    throw 'RENDER_API_KEY is missing. Add your Render API key to the ignored .env file; see .env.example.'
}

$serviceId = $env:RENDER_SERVICE_ID
if (-not $serviceId) { $serviceId = Get-DotEnvValue -Name 'RENDER_SERVICE_ID' -Path $envFile }
if (-not $serviceId) { $serviceId = 'srv-dbjp6m6tlv9c73dt868g' }
if ($serviceId -notmatch '^srv-[A-Za-z0-9]+$') {
    throw 'RENDER_SERVICE_ID must be a Render service ID beginning with srv-.'
}

$uri = "https://api.render.com/v1/services/$serviceId/$Action"
try {
    Invoke-RestMethod -Uri $uri -Method Post -Headers @{ Authorization = "Bearer $apiKey" } -TimeoutSec 30 | Out-Null
} catch {
    $statusCode = $null
    if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
        $statusCode = [int]$_.Exception.Response.StatusCode
    }
    if ($statusCode) { throw "Render service $Action failed (HTTP $statusCode). Check the API key and service ID." }
    throw "Render service $Action failed: $($_.Exception.Message)"
}

if ($Action -eq 'suspend') {
    Write-Output 'Hosted Render service suspended. Neon database data is unchanged.'
} else {
    Write-Output 'Hosted Render service resumed.'
}
