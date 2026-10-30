function Get-XraySha256FromDigestText {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$DigestText
    )

    # Xray .dgst files commonly label SHA-256 as SHA2-256.
    # Also accept SHA-256 / SHA256 and the filename form SHA256(file)=hash.
    $Text = $DigestText
    if ($Text.Length -gt 0 -and $Text[0] -eq [char]0xFEFF) { $Text = $Text.Substring(1) }
    $Pattern = '(?im)^\s*SHA2?\s*[-_ ]?\s*256\s*(?:\([^)]*\))?\s*[:=]\s*([a-f0-9]{64})\s*$'
    $Match = [regex]::Match($Text, $Pattern)

    if (-not $Match.Success) {
        $Preview = ($Text -replace '\s+', ' ').Trim()
        if ($Preview.Length -gt 240) { $Preview = $Preview.Substring(0, 240) }
        throw "Could not parse SHA-256 from Xray .dgst. Expected a SHA2-256/SHA-256/SHA256 line with 64 hex characters. Digest preview: '$Preview'"
    }

    return $Match.Groups[1].Value.ToLowerInvariant()
}
