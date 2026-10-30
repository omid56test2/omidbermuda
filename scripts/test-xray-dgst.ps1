$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'xray-digest.ps1')
$Hash = 'a' * 64
$Cases = @(
    @{ Name = 'official SHA2-256 equals'; Text = "MD5=00000000000000000000000000000000`r`nSHA1=0000000000000000000000000000000000000000`r`nSHA2-256=$Hash`r`nSHA2-512=00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000" },
    @{ Name = 'SHA-256 colon'; Text = "SHA-256: $Hash" },
    @{ Name = 'SHA256 filename form'; Text = "SHA256 (Xray-windows-64.zip) = $Hash" },
    @{ Name = 'UTF-8 BOM'; Text = ([string][char]0xFEFF) + "SHA2-256=$Hash" }
)
foreach ($Case in $Cases) {
    $Actual = Get-XraySha256FromDigestText -DigestText $Case.Text
    if ($Actual -ne $Hash) { throw "Digest parser failed case: $($Case.Name)" }
    Write-Host "PASS: $($Case.Name)" -ForegroundColor Green
}
$Rejected = $false
try { [void](Get-XraySha256FromDigestText -DigestText 'SHA1=not-a-sha256') }
catch { $Rejected = $true }
if (-not $Rejected) { throw 'Digest parser accepted a file without SHA-256.' }
Write-Host 'PASS: rejects missing SHA-256' -ForegroundColor Green
