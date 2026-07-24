# Changelog

All notable changes use the structure from Keep a Changelog. Versions follow
Semantic Versioning.

## [Unreleased]

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
