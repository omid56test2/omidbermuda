$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $Root

$XrayExe = Join-Path $Root 'xray\xray.exe'
if (-not (Test-Path $XrayExe)) {
    Write-Host 'Xray is missing. Running the official Windows setup...' -ForegroundColor Yellow
    & (Join-Path $PSScriptRoot 'setup-windows.ps1')
}

$AppExe = Join-Path $Root 'dist\omid-iran-bermuda-windows-amd64.exe'
if (-not (Test-Path $AppExe)) {
    throw "Windows application binary is missing: $AppExe. Re-extract the complete ZIP package."
}

$DataDir = Join-Path $Root 'data-preview'
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
$TokenFile = Join-Path $DataDir 'admin-token.txt'
if (Test-Path $TokenFile) {
    $AdminToken = (Get-Content $TokenFile -Raw).Trim()
} else {
    $RandomBytes = New-Object byte[] 32
    $Rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $Rng.GetBytes($RandomBytes) } finally { $Rng.Dispose() }
    $AdminToken = ([BitConverter]::ToString($RandomBytes)).Replace('-', '').ToLowerInvariant()
    [System.IO.File]::WriteAllText($TokenFile, $AdminToken, [System.Text.Encoding]::ASCII)
}
if ($AdminToken.Length -lt 32) { throw 'Saved admin token is invalid. Delete data-preview\admin-token.txt and run again.' }

$env:PORT = '8000'
$env:LISTEN_ADDR = '127.0.0.1:8000'
$env:ADMIN_TOKEN = $AdminToken
$env:DATA_FILE = (Join-Path $DataDir 'omid_state.json')
$env:XRAY_CONFIG = (Join-Path $DataDir 'xray-generated.json')
$env:XRAY_BIN = $XrayExe
$env:PUBLIC_BASE_URL = 'http://127.0.0.1:8000'
$env:XRAY_AUTOSTART = 'true'
$env:XRAY_AUTORESTART = 'true'
$env:TRUSTED_PROXY_CIDRS = ''
$env:TLS_CERT_FILE = ''
$env:TLS_KEY_FILE = ''

Write-Host ''
Write-Host 'OMID-IRAN BERMUDA - Local Windows mode' -ForegroundColor Cyan
Write-Host 'Admin UI: http://127.0.0.1:8000' -ForegroundColor Green
Write-Host "Admin token: $AdminToken" -ForegroundColor Yellow
Write-Host "Token file: $TokenFile" -ForegroundColor DarkGray
Write-Host 'Keep this token private. The local server binds only to 127.0.0.1.' -ForegroundColor DarkGray
Write-Host 'Press Ctrl+C to stop the app.' -ForegroundColor DarkGray
Write-Host ''

& $AppExe
if ($LASTEXITCODE -ne 0) {
    throw "OMID-IRAN BERMUDA exited with code $LASTEXITCODE. Check the messages above."
}
