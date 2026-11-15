# Security policy

## Supported versions

Only the latest release receives security updates during the initial
development series.

## Reporting a vulnerability

Do not open a public issue for credential disclosure, privilege escalation,
host-key validation bypass, named-pipe authorization bypass, or installer
integrity problems. Report them privately through GitHub's private
vulnerability reporting for this repository: **Security > Report a
vulnerability** at
https://github.com/bbmumford/MapFTPDrive/security/advisories/new.

Reports should include affected versions, reproduction conditions, impact,
and any suggested mitigation. Never include real endpoint credentials.

## Security design

- Endpoint passwords are kept in Windows Credential Manager, per user for
  user drives and in LocalSystem's store for system drives.
- SFTP host keys are confirmed on first use and pinned.
- System drive definitions, host keys and logs live in
  `%ProgramData%\MapFTPDrive`, whose protected ACL admits only SYSTEM and
  Administrators. The installers create or repair it, and the service
  refuses to start when anyone else can write it. A separate `drives.json`
  beside it, readable by Users, lists only each system drive's letter, ID,
  name and state so unelevated Explorer commands can recognise it.
- System operations require membership in Administrators and UAC elevation.
- Release dependencies are vendored or version-and-hash pinned.
- Both setup programs embed the exact pinned WinFsp redistributable.
  Packaging fails if its SHA-256 does not match the reviewed value, and the
  Burn bundle verifies the payload hash again before running it.
- Release artifacts include SHA-256 manifests and GitHub provenance
  attestations.

## System drives are shared with every local user

A system-wide drive is mounted by the LocalSystem service with the
credentials the administrator entered, and Windows shows it to every user
of the computer, including standard users and Remote Desktop sessions. All
of them can read, change and delete the remote files with the
administrator's FTP or SFTP account. The password itself stays in
LocalSystem's Credential Manager and is never shown to them. Use a
system-wide drive only for an account whose remote access every local user
may have; otherwise map a per-user drive, which is visible only in the
mapping user's own session.

## Control pipe threat model

Commands reach the hosts over named pipes. A mapping request carries the
new drive's password, so both ends are checked:

- Server side, each pipe's DACL admits only SYSTEM and Administrators (the
  system pipe, served by the service) or SYSTEM and the owning user (a user
  pipe, served by that user's host in one logon session). Other local users
  cannot send requests.
- Client side, pipe names are predictable and the first process to create
  a name serves it. A local process could therefore create a host's pipe
  name before the host starts and wait for a password. Before sending
  anything, the client asks Windows which process serves the pipe and
  requires LocalSystem for the system pipe, or the caller's own account in
  the caller's logon session for a user pipe; otherwise it stops with "the
  MapFTPDrive control pipe is served by an untrusted process". Clients
  connect at the anonymous impersonation level, so a server cannot act as
  them.
- Out of scope: a process already running as the same user (it can read
  that user's Credential Manager directly) and administrators or LocalSystem
  (they control the machine).

MapFTPDrive leaves an existing or bundled WinFsp installation in place when
MapFTPDrive is removed. WinFsp is a shared filesystem runtime and removing it
could break unrelated applications.
