# Architecture

## One engine, two supervisors

The executable has a single in-process mount engine. A per-user supervisor
starts at sign-in and restores user-scoped drives. The Windows service uses
the same engine to restore system-scoped drives as LocalSystem.

The engine constructs an rclone FTP or SFTP filesystem directly, layers the
rclone VFS over it, and exposes that VFS through the cgofuse/WinFsp host.
FTPS uses the FTP backend with explicit TLS. No external rclone process,
configuration file, or remote-control API is involved.

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

## Upstream source

Only the dependency closure needed for FTP, SFTP, VFS, cgofuse, and WinFsp
is vendored into `third_party`, with upstream commit IDs and license notices.
Because this creates a combined executable from GPL-covered components, the
project and distributed binaries must use a GPL-compatible distribution
model and provide corresponding source.

