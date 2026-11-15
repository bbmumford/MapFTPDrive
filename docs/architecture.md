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
live under `%ProgramData%\MapFTPDrive`. Passwords are separate Windows
Credential Manager entries.

`%ProgramData%` lets every user create files and folders, so the system
directory carries its own protected descriptor
(`O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`): owned by Administrators,
nothing inherited from `%ProgramData%`, full control for SYSTEM and
Administrators only. Anyone else who could write it could change the
endpoints the service mounts with stored passwords, or redirect the
service's file writes.

- Both installers run `MapFTPDrive.exe prepare-system-data` (the MSI as a
  LocalSystem custom action, the EXE through `install`). It creates the
  directory with that descriptor, or repairs an existing one: a junction or
  symbolic link at that path is removed rather than followed, the directory
  is locked first, and every entry another principal could have written
  (owned by someone else, writable by someone else, a reparse point, or a
  file with extra hard links) is deleted before the descriptor is
  propagated to what remains.
- The endpoint store creates the directory with the same descriptor if it
  is ever missing, instead of a plain `MkdirAll`.
- The service checks the directory and its direct entries at every start.
  If any is a reparse point, is owned by anyone other than SYSTEM,
  Administrators or TrustedInstaller, or grants write access to anyone
  else, the service stops with service-specific exit code 3 and writes
  event ID 1 from source `MapFTPDrive` to the Application log. Repairing or
  reinstalling restores the directory.

Unelevated processes, such as the Explorer verbs, cannot read the system
store. The service therefore keeps `drives.json` beside it: drive letter,
scope, ID, name and whether the drive is unmounted, with no server, user
name, path, host key or credential reference. It is rewritten after every
system store save and at every service start, and carries its own
descriptor (`O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)`) so Users
may read but not change it. Elevated processes read the store itself.

Unmount transitions a definition to inactive but retains it. Remove
unmounts it, removes the definition, and deletes its credential.

## Explorer integration

Static verbs are registered against the `CLSID_MyComputer` shell object for
mapping (**Map FTP Drive**, **Map FTP Drive (system-wide)**) and for
remounting a drive that **Unmount** disabled (**Mount FTP Drive**, which
lists the unmounted managed drives: an unmounted drive has no drive item to
right-click). The system-wide mapping verb is marked `Extended`, so it
appears only in the extended (Shift) menu on classic Windows Explorer.

Managed drive verbs (**Unmount**, **Rename**, **Remove**) are registered on
`Drive\shell` with an `AppliesTo` condition:

```text
System.Volume.FileSystem:="FUSE-MapFTP"
```

Every MapFTPDrive mount passes `--FileSystemName=MapFTP` to WinFsp, which
reports the volume's file system as `FUSE-MapFTP` (WinFsp adds the `FUSE-`
prefix and keeps at most 15 characters). Explorer evaluates the condition per
drive, so the verbs never appear on C:, removable media, SMB shares or other
FUSE file systems. The condition is a display filter only: each command
still resolves the selected drive against the managed endpoint stores and
refuses anything else.

Explorer expands `%V` for a drive to its root, such as `X:\`. The verbs pass
`--drive "%V."`: quoting `"%V"` alone would let the root's trailing
backslash escape the closing quote, and the program would receive `X:"`.
The drive argument is normalised to a single letter (`X`, `X:`, `X:\`,
`X:\.` and the legacy `X:"` are accepted; paths are rejected).
`internal/explorer/verbs_test.go` parses the exact command strings from the
Go verb table and both installer sources with the argv rules Go uses on
Windows.

Explorer starts these commands without a console. Errors from map, mount,
unmount, rename and remove are therefore shown in a message box; installer
and service commands report failure only through their exit code.

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
