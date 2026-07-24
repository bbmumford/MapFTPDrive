[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$WinFspUrl = "https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi",
    [string]$WinFspSha256 = "073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $repo "build/MapFTPDrive.exe"
$winfsp = Join-Path $repo "build/winfsp.msi"
$output = Join-Path $repo "installer/output"
$wxs = Join-Path $repo "installer/MapFTPDrive.wxs"
$wixArch = if ($Architecture -eq "arm64") { "arm64" } else { "x64" }
$signed = [bool]($env:WINDOWS_SIGNING_PFX_BASE64 -and $env:WINDOWS_SIGNING_PASSWORD)
$suffix = if ($signed) { "" } else { "-unsigned" }
$msi = Join-Path $output "MapFTPDrive-$Version-windows-$wixArch$suffix.msi"

if (-not (Test-Path $binary)) { throw "Application binary not found at $binary" }
New-Item -ItemType Directory -Force -Path $output | Out-Null

if (-not (Test-Path $winfsp)) {
    Invoke-WebRequest -Uri $WinFspUrl -OutFile $winfsp
}
$actualHash = (Get-FileHash -Algorithm SHA256 $winfsp).Hash.ToLowerInvariant()
if ($actualHash -ne $WinFspSha256.ToLowerInvariant()) {
    throw "WinFsp checksum mismatch: expected $WinFspSha256, got $actualHash"
}

& wix build -arch $wixArch -d "Version=$Version" -d "Binary=$binary" -d "WinFspMsi=$winfsp" -d "RepoRoot=$repo" -o $msi $wxs
if ($LASTEXITCODE -ne 0) { throw "WiX MSI build failed" }

if ($signed) {
    $pfx = Join-Path $env:RUNNER_TEMP "codesign-msi.pfx"
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_SIGNING_PFX_BASE64))
    & signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $pfx /p $env:WINDOWS_SIGNING_PASSWORD $msi
    if ($LASTEXITCODE -ne 0) { throw "MSI signing failed" }
    Remove-Item -Force $pfx
}

Get-FileHash -Algorithm SHA256 $msi |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $(Split-Path -Leaf $msi)" } |
    Set-Content -Encoding ascii "$msi.sha256"

Write-Output $msi
