[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$Version
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $repo "build/MapFTPDrive.exe"
$output = Join-Path $repo "installer/output"
$wxs = Join-Path $repo "installer/MapFTPDrive.wxs"
$wixArch = if ($Architecture -eq "arm64") { "arm64" } else { "x64" }
$signed = [bool]($env:WINDOWS_SIGNING_PFX_BASE64 -and $env:WINDOWS_SIGNING_PASSWORD)
$suffix = if ($signed) { "" } else { "-unsigned" }
$msi = Join-Path $output "MapFTPDrive-$Version-windows-$wixArch$suffix.msi"

if (-not (Test-Path $binary)) { throw "Application binary not found at $binary" }
New-Item -ItemType Directory -Force -Path $output | Out-Null

& wix build -arch $wixArch -d "Version=$Version" -d "Binary=$binary" -d "RepoRoot=$repo" -o $msi $wxs
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
