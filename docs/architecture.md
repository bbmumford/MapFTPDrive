# Architecture

## One engine, two supervisors

The executable has a single in-process mount engine. A per-user supervisor
starts at sign-in and restores user-scoped drives. The Windows service uses
the same engine to restore system-scoped drives as LocalSystem.

The engine constructs an rclone FTP or SFTP filesystem directly, layers the
rclone VFS over it, and exposes that VFS through the cgofuse/WinFsp host.
FTPS uses the FTP backend with explicit TLS. No external rclone process,
configuration file, or remote-control API is involved.

WinFsp remains a separately serviced Windows filesystem driver rather than
being linked into the Go executable. It is delivered inside every
MapFTPDrive installer, so users do not have to locate or install it
separately.

## Elevation

The normal Explorer verb is registered under HKCU and opens the mapper
unelevated. The extended verb has the `Extended` and `HasLUAShield` registry
values and relaunches the system-scope operation with `runas`. The elevated
process sends the definition and a protected credential to the installed
service. Merely holding Shift does not silently grant system scope; Windows
still displays its UAC consent UI.

## Persistence

User definitions live under `%LOCALAPPDATA%\MapFTPDrive`. System definitions
live under `%ProgramData%\MapFTPDrive` with an ACL limited to Administrators
and SYSTEM. Passwords are separate Windows Credential Manager entries.

Unmount transitions a definition to inactive but retains it. Remove
unmounts it, removes the definition, and deletes its credential.

## Explorer integration

Static verbs are registered against the `CLSID_MyComputer` shell object for
mapping. Managed drive verbs are registered with an application command
that validates the selected drive against the endpoint store before acting.
The system-wide mapping verb is marked `Extended`, so it appears only in the
extended (Shift) menu on classic Windows Explorer.

## Installation and dependencies

The Inno Setup EXE and WiX MSI both embed the same pinned WinFsp 2.1
redistributable. The build downloads that exact upstream MSI and verifies
its SHA-256 before packaging.

The EXE runs WinFsp synchronously before registering MapFTPDrive. An MSI
cannot start another MSI while its own transaction owns the global Windows
Installer mutex. The MapFTPDrive MSI therefore installs the WinFsp payload
alongside the application and starts `install-dependencies` as LocalSystem
without waiting. That helper:

1. checks the 32-bit `HKLM\Software\WinFsp` installation entry;
2. returns immediately when WinFsp is already installed;
3. invokes the embedded MSI silently when it is absent; and
4. retries Windows Installer error 1618 until the parent transaction exits.

The runtime payload remains owned by the MapFTPDrive MSI for repair and
upgrade integrity. MapFTPDrive uninstall does not remove the shared WinFsp
runtime.

Release packaging builds x64 and ARM64 setup EXEs and MSIs. Authenticode
signing is enabled only when both signing secrets are present. Unsigned
artifacts receive `-unsigned`; signed artifacts receive no suffix. SHA-256
files, a combined manifest, and provenance attestations accompany releases.

## Upstream source

The Go dependency closure needed for FTP, SFTP, VFS, and cgofuse is retained
in `vendor`. Selected WinFsp source/provenance and its unmodified license are
retained in `third_party/winfsp`; the installer carries the unmodified,
hash-pinned upstream WinFsp redistributable. Because the application combines
GPL-covered components, the project and distributed binaries use a
GPL-compatible distribution model and retain corresponding source and
notices.
