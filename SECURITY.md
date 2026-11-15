# Security policy

## Supported versions

Only the latest release receives security updates during the initial
development series.

## Reporting a vulnerability

Do not open a public issue for credential disclosure, privilege escalation,
host-key validation bypass, named-pipe authorization bypass, or installer
integrity problems. Use GitHub's private vulnerability reporting feature for
this repository.

Reports should include affected versions, reproduction conditions, impact,
and any suggested mitigation. Never include real endpoint credentials.

## Security design

- Endpoint passwords are kept in Windows Credential Manager.
- SFTP host keys are confirmed on first use and pinned.
- User and system control pipes have explicit SID-based ACLs.
- System operations require membership in Administrators and UAC elevation.
- Release dependencies are vendored or version-and-hash pinned.
- Both setup programs embed the exact pinned WinFsp redistributable.
  Packaging fails if its SHA-256 does not match the reviewed value, and the
  Burn bundle verifies the payload hash again before running it.
- Release artifacts include SHA-256 manifests and GitHub provenance
  attestations.

MapFTPDrive leaves an existing or bundled WinFsp installation in place when
MapFTPDrive is removed. WinFsp is a shared filesystem runtime and removing it
could break unrelated applications.
