[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Commit = "unknown",
    [string]$BuildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"),
    [string]$WinFspUrl = "https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi",
    [string]$WinFspSha256 = "073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$build = Join-Path $repo "build"
$output = Join-Path $repo "installer/output"
$binary = Join-Path $build "MapFTPDrive.exe"
$winfsp = Join-Path $build "winfsp.msi"

New-Item -ItemType Directory -Force -Path $build, $output | Out-Null

# Embed the manifest, icon and this build's version in the executable.
& (Join-Path $PSScriptRoot "generate-resources.ps1") -Version $Version

$env:GOOS = "windows"
$env:GOARCH = $Architecture
$env:CGO_ENABLED = "0"
$ldflags = @(
    "-s", "-w", "-H=windowsgui",
    "-X", "mapftpdrive/internal/buildinfo.Version=$Version",
    "-X", "mapftpdrive/internal/buildinfo.Commit=$Commit",
    "-X", "mapftpdrive/internal/buildinfo.Date=$BuildDate"
) -join " "

& go build -mod=vendor -trimpath -buildvcs=true -ldflags $ldflags -o $binary ./cmd/mapftpdrive
if ($LASTEXITCODE -ne 0) { throw "Go build failed" }

Invoke-WebRequest -Uri $WinFspUrl -OutFile $winfsp
$actualHash = (Get-FileHash -Algorithm SHA256 $winfsp).Hash.ToLowerInvariant()
if ($actualHash -ne $WinFspSha256.ToLowerInvariant()) {
    throw "WinFsp checksum mismatch: expected $WinFspSha256, got $actualHash"
}

$signed = [bool]($env:WINDOWS_SIGNING_PFX_BASE64 -and $env:WINDOWS_SIGNING_PASSWORD)
if ($signed) {
    $pfx = Join-Path $env:RUNNER_TEMP "codesign.pfx"
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_SIGNING_PFX_BASE64))
    & signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $pfx /p $env:WINDOWS_SIGNING_PASSWORD $binary
    if ($LASTEXITCODE -ne 0) { throw "Executable signing failed" }
}

$iscc = "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe"
if (-not (Test-Path $iscc)) { throw "Inno Setup compiler not found at $iscc" }
$innoArch = if ($Architecture -eq "arm64") { "arm64" } else { "x64" }
$signingSuffix = if ($signed) { "" } else { "-unsigned" }
& $iscc "/DVersion=$Version" "/DArch=$innoArch" "/DBinary=$binary" "/DWinFspMsi=$winfsp" "/DSigningSuffix=$signingSuffix" (Join-Path $repo "installer/MapFTPDrive.iss")
if ($LASTEXITCODE -ne 0) { throw "Installer build failed" }

$installer = Get-ChildItem $output -Filter "MapFTPDrive-*-setup*.exe" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $installer) { throw "Installer output was not created" }

if ($signed) {
    & signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $pfx /p $env:WINDOWS_SIGNING_PASSWORD $installer.FullName
    if ($LASTEXITCODE -ne 0) { throw "Installer signing failed" }
    Remove-Item -Force $pfx
}

Get-FileHash -Algorithm SHA256 $installer.FullName |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $($installer.Name)" } |
    Set-Content -Encoding ascii "$($installer.FullName).sha256"

Write-Output $installer.FullName
