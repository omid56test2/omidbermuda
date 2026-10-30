$ErrorActionPreference = 'Stop'
$Version = '26.10.21'
$Asset = 'Xray-windows-64.zip'
$BaseUrl = "https://github.com/XTLS/Xray-core/releases/download/v$Version"
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$XrayDir = Join-Path $Root 'xray'
$Target = Join-Path $XrayDir 'xray.exe'
$Temp = Join-Path $env:TEMP ("omid-xray-" + [guid]::NewGuid().ToString('N'))

New-Item -ItemType Directory -Force -Path $XrayDir, $Temp | Out-Null
try {
    $ZipPath = Join-Path $Temp $Asset
    $DigestPath = Join-Path $Temp ($Asset + '.dgst')
    Write-Host "Downloading official Xray $Version for Windows x64..." -ForegroundColor Cyan
    Invoke-WebRequest -Uri "$BaseUrl/$Asset" -OutFile $ZipPath -UseBasicParsing
    Invoke-WebRequest -Uri "$BaseUrl/$Asset.dgst" -OutFile $DigestPath -UseBasicParsing

    $DigestText = Get-Content $DigestPath -Raw
    $Match = [regex]::Match($DigestText, '(?im)\bSHA-?256\b(?:\s*\([^)]*\))?\s*[:=]\s*([a-f0-9]{64})')
    if (-not $Match.Success) {
        throw 'Could not read SHA-256 from the official Xray .dgst file. The archive will not be installed.'
    }
    $Expected = $Match.Groups[1].Value.ToLowerInvariant()
    $Actual = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) {
        throw "Xray archive SHA-256 mismatch. Expected $Expected but received $Actual."
    }

    $ExtractDir = Join-Path $Temp 'extracted'
    Expand-Archive -LiteralPath $ZipPath -DestinationPath $ExtractDir -Force
    $XrayExe = Get-ChildItem -Path $ExtractDir -Filter 'xray.exe' -Recurse | Select-Object -First 1
    if (-not $XrayExe) { throw 'xray.exe was not found in the official archive.' }
    Copy-Item -LiteralPath $XrayExe.FullName -Destination $Target -Force
    Write-Host "Installed and checksum-verified: $Target" -ForegroundColor Green
    & $Target version
    if ($LASTEXITCODE -ne 0) { throw 'Xray executable did not start successfully.' }
}
finally {
    Remove-Item -LiteralPath $Temp -Recurse -Force -ErrorAction SilentlyContinue
}
