<#
Regenerates cmd/mapftpdrive/rsrc_windows_{amd64,arm64}.syso from
cmd/mapftpdrive/winres (application manifest, icon and version
information). `go build` links the .syso matching GOARCH into
MapFTPDrive.exe. build-installer.ps1 runs this with the release version;
run it by hand after editing winres.json or the icons, and commit the
regenerated .syso files built with the default 0.0.0-dev version.

The manifest enables Common Controls 6 (required by the walk dialogs),
per-monitor v2 DPI awareness and asInvoker execution, and declares
Windows 10/11 support. Explorer verbs use icon index 0 of the executable.
#>
[CmdletBinding()]
param(
    [string]$Version = "0.0.0-dev",
    [string]$GoWinresVersion = "v0.3.3"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$package = Join-Path $repo "cmd/mapftpdrive"

if ($Version -notmatch '^\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]+)?$') {
    throw "Version '$Version' is not MAJOR.MINOR.PATCH with an optional suffix"
}

$savedGoFlags = $env:GOFLAGS
Push-Location $package
try {
    # `go run module@version` builds outside this module, where the
    # repository's -mod=vendor setting does not apply. The module checksum
    # database verifies the pinned go-winres version.
    $env:GOFLAGS = ""
    # go-winres stores the numeric part (1.2.3.0) in VS_FIXEDFILEINFO and the
    # full string, including any -suffix, in FileVersion/ProductVersion.
    & go run "github.com/tc-hib/go-winres@$GoWinresVersion" make `
        --in "winres/winres.json" `
        --out "rsrc" `
        --arch "amd64,arm64" `
        --product-version $Version `
        --file-version $Version
    if ($LASTEXITCODE -ne 0) { throw "go-winres failed" }
}
finally {
    Pop-Location
    $env:GOFLAGS = $savedGoFlags
}
