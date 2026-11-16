# Changelog

All notable changes use the structure from Keep a Changelog. Versions follow
Semantic Versioning.

## [Unreleased]

## [0.2.0] - 2026-11-16

### Fixed

- Explorer's drive commands (Unmount, Rename, Remove) receive the right drive;
  they show only on MapFTPDrive drives, and their errors appear in a message box.
- The mapping and management windows open: the executable carries its
  manifest, version information and icon.
- `%ProgramData%\MapFTPDrive` is limited to SYSTEM and Administrators; the
  service refuses to start when anyone else can write there, and drive IDs
  can no longer name paths outside it.
- A password is sent only to a control pipe served by SYSTEM, or by the same
  user in the same session; each signed-in session has its own pipe.
- Drives reconnect with backoff, and at once when the network changes; a
  dropped drive is mounted again instead of being forgotten.
- A mapping is saved only after its server identity is pinned and a test
  mount succeeds; a failed one leaves nothing behind.
- One background process per user, which takes its pipe before mounting.
- Explorer's menu is registered for every user, and the service keeps its
  settings and restarts across upgrades.
- Office and other in-place saves work: rclone's write cache is on.

### Added

- A management window (Start menu → Manage FTP Drives) to map, mount,
  unmount, edit, rename, remove, test and re-pin drives, with each drive's
  status.
- Mount FTP Drive on This PC, for drives that are mapped but not mounted.
- SFTP private-key sign-in, and pinning of FTPS certificates Windows does not
  trust, with a prompt when a server's identity changes.
- A setup bundle that installs WinFsp only when needed, beside the plain MSI.
- Uninstall stops every drive and removes stored passwords and data unless
  told to keep them.
- Logs per user and for the service, and Event Log entries for the service.

### Changed

- Go 1.27, rclone 1.76.0 and current dependencies; no known vulnerabilities.
- SFTP drives always verify the server's host key, and SFTP remote shell
  commands are off.
- Releases build reproducibly, are gated on Windows CI and an install test on
  x64 and ARM64, and run each job with only the permissions it needs.

## [0.1.1] - 2026-07-24

### Fixed

- Embedded the pinned, checksum-verified WinFsp 2.1 runtime in the MSI.
- Removed the MSI launch condition that rejected clean computers without
  WinFsp.
- Added a LocalSystem post-install dependency helper that waits for the
  parent Windows Installer transaction before installing WinFsp silently.
- Preserved clean artifact names for signed releases; only unsigned builds
  receive the `-unsigned` suffix.

## [0.1.0] - 2026-07-24

### Added

- Native Windows mapper for FTP, explicit/implicit FTPS, and SFTP.
- In-process rclone VFS and WinFsp mount engine.
- Persistent user host and LocalSystem service host.
- ACL-protected named-pipe control channel.
- Windows Credential Manager secret storage.
- SFTP host-key discovery, user confirmation, and pinning.
- Explorer verbs for user/system mapping, mount, unmount, rename, and remove.
- Persistent detection of drive labels changed through Explorer Rename.
- x64 and ARM64 EXE/MSI release packaging, checksums, optional Authenticode
  signing, CodeQL, and build provenance attestations.

[Unreleased]: https://github.com/bbmumford/MapFTPDrive/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/bbmumford/MapFTPDrive/releases/tag/v0.1.1
[0.1.0]: https://github.com/bbmumford/MapFTPDrive/releases/tag/v0.1.0
