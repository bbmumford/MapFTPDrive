<#
Authenticode signing helpers, dot-sourced by build-installer.ps1 and
build-msi.ps1.

Signing is enabled by either environment configuration:

- WINDOWS_SIGNING_CERT_THUMBPRINT: the SHA-1 thumbprint of a code-signing
  certificate whose private key is already available in the CurrentUser\My
  or LocalMachine\My store, for example one backed by a hardware token.
- WINDOWS_SIGNING_PFX_BASE64 and WINDOWS_SIGNING_PASSWORD: a base64 PFX and
  its password, as the release workflow supplies from repository secrets.
  Start-CodeSigning imports the PFX into CurrentUser\My in-process (its
  intermediate certificates into CurrentUser\CA) and Stop-CodeSigning removes
  what it imported, including the private key.

Either way signtool selects the certificate by thumbprint, so neither the
password nor the PFX is written to disk or placed on a process command line.
With neither configuration the scripts build unsigned artifacts named with
the -unsigned suffix.
#>

$script:CodeSigningTimestampUrl = "http://timestamp.digicert.com"

# Test-CodeSigningRequested reports whether the environment asks for signed
# artifacts; the build scripts use it to choose the -unsigned suffix.
function Test-CodeSigningRequested {
    return [bool]($env:WINDOWS_SIGNING_CERT_THUMBPRINT -or
        ($env:WINDOWS_SIGNING_PFX_BASE64 -and $env:WINDOWS_SIGNING_PASSWORD))
}

# Find-SignTool returns signtool.exe from PATH or, as on hosted runners where
# the Windows SDK is installed but not on PATH, the newest SDK copy for the
# host architecture.
function Find-SignTool {
    $command = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    $kits = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin"
    $hostArch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "x64" }
    $candidate = Get-ChildItem $kits -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^10\.[0-9.]+$' } |
        Sort-Object { [version]$_.Name } -Descending |
        ForEach-Object { Join-Path $_.FullName "$hostArch\signtool.exe" } |
        Where-Object { Test-Path $_ } |
        Select-Object -First 1
    if (-not $candidate) { throw "signtool.exe was not found on PATH or under $kits" }
    return $candidate
}

# Start-CodeSigning prepares the configured certificate and returns the state
# Invoke-SignTool and Stop-CodeSigning need, or $null when signing is not
# configured.
function Start-CodeSigning {
    if (-not (Test-CodeSigningRequested)) { return $null }
    $signing = [pscustomobject]@{
        SignTool     = Find-SignTool
        Thumbprint   = $null
        MachineStore = $false
        Imported     = [System.Collections.Generic.List[string]]::new()
    }

    if ($env:WINDOWS_SIGNING_CERT_THUMBPRINT) {
        $thumbprint = ($env:WINDOWS_SIGNING_CERT_THUMBPRINT -replace '\s', '').ToUpperInvariant()
        if (Test-Path "Cert:\LocalMachine\My\$thumbprint") {
            $signing.MachineStore = -not (Test-Path "Cert:\CurrentUser\My\$thumbprint")
        } elseif (-not (Test-Path "Cert:\CurrentUser\My\$thumbprint")) {
            throw "No certificate with thumbprint $thumbprint is in CurrentUser\My or LocalMachine\My"
        }
        $signing.Thumbprint = $thumbprint
        return $signing
    }

    $flags = [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::UserKeySet -bor
        [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::PersistKeySet
    $certificates = [System.Security.Cryptography.X509Certificates.X509Certificate2Collection]::new()
    $certificates.Import([Convert]::FromBase64String($env:WINDOWS_SIGNING_PFX_BASE64), $env:WINDOWS_SIGNING_PASSWORD, $flags)
    $signers = @($certificates | Where-Object { $_.HasPrivateKey })
    if ($signers.Count -ne 1) {
        throw "The signing PFX must contain exactly one certificate with a private key; it has $($signers.Count)"
    }
    try {
        foreach ($certificate in $certificates) {
            $storeName = if ($certificate.HasPrivateKey) { "My" } else { "CA" }
            $path = "Cert:\CurrentUser\$storeName\$($certificate.Thumbprint)"
            # A certificate that was already present stays after the build.
            if (Test-Path $path) { continue }
            $store = [System.Security.Cryptography.X509Certificates.X509Store]::new($storeName, "CurrentUser")
            $store.Open("ReadWrite")
            try { $store.Add($certificate) } finally { $store.Close() }
            $signing.Imported.Add($path)
        }
    } catch {
        Stop-CodeSigning $signing
        throw
    }
    $signing.Thumbprint = $signers[0].Thumbprint
    return $signing
}

# Invoke-SignTool signs and timestamps one file with the certificate
# Start-CodeSigning selected.
function Invoke-SignTool {
    param(
        [Parameter(Mandatory = $true)]$Signing,
        [Parameter(Mandatory = $true)][string]$Path
    )
    $arguments = @("sign", "/sha1", $Signing.Thumbprint, "/s", "My")
    if ($Signing.MachineStore) { $arguments += "/sm" }
    $arguments += @("/fd", "SHA256", "/td", "SHA256", "/tr", $script:CodeSigningTimestampUrl, $Path)
    & $Signing.SignTool @arguments
    if ($LASTEXITCODE -ne 0) { throw "Signing $Path failed" }
}

# Stop-CodeSigning removes every certificate Start-CodeSigning imported and
# the imported private key. It accepts $null so callers can run it
# unconditionally.
function Stop-CodeSigning {
    param($Signing)
    if (-not $Signing) { return }
    foreach ($path in $Signing.Imported) {
        if (-not (Test-Path $path)) { continue }
        if ($path -like "Cert:\CurrentUser\My\*") {
            Remove-Item $path -DeleteKey
        } else {
            Remove-Item $path
        }
    }
    $Signing.Imported.Clear()
}

# Invoke-CodeSigning signs and timestamps each path with the configured
# certificate, removing any imported certificate afterwards even when signing
# fails. The build scripts call it only when Test-CodeSigningRequested is
# true.
function Invoke-CodeSigning {
    param([Parameter(Mandatory = $true)][string[]]$Path)
    $signing = Start-CodeSigning
    if (-not $signing) { throw "Code signing is not configured" }
    try {
        foreach ($file in $Path) { Invoke-SignTool $signing $file }
    } finally {
        Stop-CodeSigning $signing
    }
}
