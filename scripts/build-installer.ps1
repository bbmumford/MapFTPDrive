[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Commit = "unknown",
    # Empty selects SOURCE_DATE_EPOCH or, failing that, the HEAD commit time,
    # so rebuilding a commit embeds the same date.
    [string]$BuildDate = "",
    [string]$WinFspUrl = "https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi",
    [string]$WinFspSha256 = "073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$build = Join-Path $repo "build"
$output = Join-Path $repo "installer/output"
$binary = Join-Path $build "MapFTPDrive.exe"
$winfsp = Join-Path $build "winfsp.msi"
. (Join-Path $PSScriptRoot "codesign.ps1")

if (-not $BuildDate) {
    $epoch = $env:SOURCE_DATE_EPOCH
    if (-not $epoch) {
        $epoch = & git -C $repo log -1 --format=%ct HEAD
        if ($LASTEXITCODE -ne 0 -or -not $epoch) {
            throw "Set SOURCE_DATE_EPOCH or -BuildDate; the HEAD commit time is unavailable"
        }
    }
    $BuildDate = [DateTimeOffset]::FromUnixTimeSeconds([long]$epoch).UtcDateTime.ToString("yyyy-MM-ddTHH:mm:ssZ")
}

New-Item -ItemType Directory -Force -Path $build, $output | Out-Null

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

$signed = Test-CodeSigningRequested
if ($signed) {
    Invoke-CodeSigning $binary
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
    Invoke-CodeSigning $installer.FullName
}

Get-FileHash -Algorithm SHA256 $installer.FullName |
    ForEach-Object { "$($_.Hash.ToLowerInvariant())  $($installer.Name)" } |
    Set-Content -Encoding ascii "$($installer.FullName).sha256"

Write-Output $installer.FullName
