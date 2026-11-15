[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$WinFspUrl = "https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi",
    [string]$WinFspSha256 = "073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a"
)

# Builds two artifacts from build/MapFTPDrive.exe (made by build-installer.ps1):
#   MapFTPDrive-<version>-windows-<arch>.msi
#     the application alone, for administrators who deploy WinFsp themselves;
#     it refuses to install while WinFsp is absent.
#   MapFTPDrive-<version>-windows-<arch>-bundle-setup.exe
#     a WiX Burn bundle that installs the pinned WinFsp MSI when no equal or
#     newer WinFsp is present and then the MSI above.
$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
$binary = Join-Path $repo "build/MapFTPDrive.exe"
$winfsp = Join-Path $repo "build/winfsp.msi"
$output = Join-Path $repo "installer/output"
$wxs = Join-Path $repo "installer/MapFTPDrive.wxs"
$bundleWxs = Join-Path $repo "installer/MapFTPDrive.bundle.wxs"
$wixArch = if ($Architecture -eq "arm64") { "arm64" } else { "x64" }
. (Join-Path $PSScriptRoot "codesign.ps1")
$signed = Test-CodeSigningRequested
$suffix = if ($signed) { "" } else { "-unsigned" }
$msi = Join-Path $output "MapFTPDrive-$Version-windows-$wixArch$suffix.msi"
$bundle = Join-Path $output "MapFTPDrive-$Version-windows-$wixArch-bundle-setup$suffix.exe"

# Windows Installer and Burn accept only numeric versions; a -suffix such as
# -dev stays in the file names.
if ($Version -notmatch '^(\d+\.\d+\.\d+)') { throw "Version '$Version' does not start with MAJOR.MINOR.PATCH" }
$packageVersion = $Matches[1]
# The bundle skips WinFsp when this version or a newer one is installed.
if ($WinFspUrl -notmatch 'winfsp-(\d+\.\d+\.\d+)\.msi$') { throw "Cannot read the WinFsp version from $WinFspUrl" }
$winfspVersion = $Matches[1]

if (-not (Test-Path $binary)) { throw "Application binary not found at $binary" }
New-Item -ItemType Directory -Force -Path $output | Out-Null

if (-not (Test-Path $winfsp)) {
    Invoke-WebRequest -Uri $WinFspUrl -OutFile $winfsp
}
$actualHash = (Get-FileHash -Algorithm SHA256 $winfsp).Hash.ToLowerInvariant()
if ($actualHash -ne $WinFspSha256.ToLowerInvariant()) {
    throw "WinFsp checksum mismatch: expected $WinFspSha256, got $actualHash"
}

# The bundle needs the Util (product search) and BootstrapperApplications
# (standard UI) extensions at the version of the WiX tool in use.
$wixVersion = ((& wix --version | Select-Object -Last 1) -split '\+')[0].Trim()
if ($LASTEXITCODE -ne 0 -or -not $wixVersion) { throw "Cannot determine the WiX version" }
foreach ($extension in "WixToolset.Util.wixext", "WixToolset.BootstrapperApplications.wixext") {
    & wix extension add -g "$extension/$wixVersion"
    if ($LASTEXITCODE -ne 0) { throw "Adding WiX extension $extension/$wixVersion failed" }
}

& wix build -arch $wixArch -d "Version=$packageVersion" -d "Binary=$binary" -d "RepoRoot=$repo" -o $msi $wxs
if ($LASTEXITCODE -ne 0) { throw "WiX MSI build failed" }

if ($signed) {
    Invoke-CodeSigning $msi
}

# The bundle embeds the MSI signed above.
& wix build -arch $wixArch `
    -ext WixToolset.Util.wixext `
    -ext WixToolset.BootstrapperApplications.wixext `
    -d "Version=$packageVersion" `
    -d "WinFspMsi=$winfsp" `
    -d "WinFspVersion=$winfspVersion" `
    -d "MapFTPDriveMsi=$msi" `
    -o $bundle $bundleWxs
if ($LASTEXITCODE -ne 0) { throw "WiX bundle build failed" }

if ($signed) {
    # Burn runs its engine from a copy it extracts, so the engine inside the
    # bundle is signed first, reattached, and then the whole bundle is signed.
    $pfx = Join-Path $env:RUNNER_TEMP "codesign-bundle.pfx"
    $engine = Join-Path $env:RUNNER_TEMP "MapFTPDrive-bundle-engine.exe"
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_SIGNING_PFX_BASE64))
    & wix burn detach $bundle -engine $engine
    if ($LASTEXITCODE -ne 0) { throw "Detaching the bundle engine failed" }
    & signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $pfx /p $env:WINDOWS_SIGNING_PASSWORD $engine
    if ($LASTEXITCODE -ne 0) { throw "Bundle engine signing failed" }
    & wix burn reattach $bundle -engine $engine -o $bundle
    if ($LASTEXITCODE -ne 0) { throw "Reattaching the bundle engine failed" }
    & signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $pfx /p $env:WINDOWS_SIGNING_PASSWORD $bundle
    if ($LASTEXITCODE -ne 0) { throw "Bundle signing failed" }
    Remove-Item -Force $pfx, $engine
}

foreach ($artifact in $msi, $bundle) {
    Get-FileHash -Algorithm SHA256 $artifact |
        ForEach-Object { "$($_.Hash.ToLowerInvariant())  $(Split-Path -Leaf $artifact)" } |
        Set-Content -Encoding ascii "$artifact.sha256"
}

Write-Output $msi
Write-Output $bundle
