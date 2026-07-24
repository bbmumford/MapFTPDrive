# MapFTPDrive agent guide

## Mission

Build a polished Windows application that maps FTP, explicit/implicit FTPS,
and SFTP endpoints as persistent Explorer drives. The application embeds its
protocol, VFS, and WinFsp host code in-process. Never regress to spawning or
requiring an external `rclone.exe`.

## Product invariants

- User mounts run without elevation and are visible only in that logon
  session.
- System mounts require UAC consent, run under the Windows service, and are
  available system-wide.
- Passwords, private-key passphrases, and tokens belong in Windows Credential
  Manager. Never place secrets in JSON, logs, process arguments, registry
  command strings, crash reports, or test fixtures.
- “Unmount” stops a mount but retains its definition. “Remove” also deletes
  the definition and credential after a clear confirmation.
- Explorer verbs must validate selected drive IDs against managed state;
  arbitrary paths must never become command or registry input.
- FTP is inherently unencrypted. Make that visible in the UI and prefer FTPS
  or SFTP.
- Preserve GPLv3 source availability, upstream notices, and exact provenance
  whenever vendored or extracted source changes.

## Architecture

- `cmd/mapftpdrive`: process modes and command dispatch only.
- `internal/config`: versioned, validated persisted models.
- `internal/credentials`: secret storage abstraction.
- `internal/explorer`: shell registration and verb handling.
- `internal/mount`: in-process rclone VFS/cgofuse/WinFsp engine.
- `internal/service`: Windows service and mount lifecycle supervision.
- `internal/ui`: native mapping and management interface.
- `installer`: installer definitions and packaging assets.
- `third_party`: selected WinFsp source and upstream provenance.
- `vendor`: reproducible Go dependency closure.

Keep business rules out of platform entry points. Prefer small interfaces at
Windows API boundaries so lifecycle and persistence behavior can be tested on
non-Windows hosts.

## Required checks

Before committing:

```text
gofmt -w cmd internal
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go test -mod=vendor ./...
GOOS=windows GOARCH=arm64 go test -mod=vendor ./...
```

On Windows, also run `go test -race -mod=vendor ./...` where supported and
exercise install, upgrade, repair, uninstall, user mount, elevated mount,
logout/login restoration, service restart restoration, credential deletion,
and occupied-drive-letter handling.

## Release policy

- Release only from an annotated `vMAJOR.MINOR.PATCH` tag.
- Do not publish when tests, installer smoke checks, vulnerability scanning,
  checksums, or provenance generation fail.
- Never commit signing certificates. GitHub secrets may supply an encrypted
  PFX and password; unsigned artifacts must be clearly identified as such.
- Pin third-party Actions to reviewed major versions and external binary
  downloads to both a version and SHA-256.
- The checked-in vendor tree is authoritative for release builds. Do not run
  an implicit dependency update during packaging.
- Update `CHANGELOG.md`, provenance records, screenshots, and user
  documentation before a stable release.

## Source changes

Do not hand-edit generated installer outputs or binaries. When changing
extracted rclone/WinFsp code, record the upstream commit and local delta in
the relevant `UPSTREAM.md`. Keep changes narrowly scoped so rebasing onto a
security update remains auditable.

