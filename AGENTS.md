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
- Both release installer formats are all-in-one: a clean supported Windows
  machine must not require a separate WinFsp download or preinstallation.
- WinFsp is a shared machine runtime. Detect an existing installation, never
  remove it during MapFTPDrive uninstall, and never attempt a synchronous
  nested MSI transaction.

## Architecture

- `cmd/mapftpdrive`: process modes and command dispatch only.
- `internal/config`: versioned, validated persisted models.
- `internal/credentials`: secret storage abstraction.
- `internal/explorer`: shell registration and verb handling.
- `internal/mount`: in-process rclone VFS/cgofuse/WinFsp engine.
- `internal/service`: Windows service and mount lifecycle supervision.
- `internal/ui`: native mapping and management interface.
- `installer`: installer definitions and packaging assets.
- `scripts`: reproducible build, dependency verification, signing, and
  packaging entry points.
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
GOOS=windows GOARCH=amd64 go vet -mod=vendor ./...
GOOS=windows GOARCH=arm64 go vet -mod=vendor ./...
GOOS=windows golangci-lint run ./...
```

`.golangci.yml` is the lint gate CI applies; use the golangci-lint version
pinned in `.github/workflows/ci.yml`. On Windows, also run
`go test -race -mod=vendor ./...` (cgo with the WinFsp Developer headers on
`CPATH`, as CI does) and
exercise install, upgrade, repair, uninstall, user mount, elevated mount,
logout/login restoration, service restart restoration, credential deletion,
and occupied-drive-letter handling.

Installer changes additionally require a release-workflow dry run for both
x64 and ARM64. Smoke-test both output formats on clean Windows without
WinFsp and on Windows with WinFsp already installed. Confirm the MSI's
asynchronous dependency helper handles Windows Installer error 1618, WinFsp
is usable before the first mount, repair succeeds, and uninstall leaves the
shared WinFsp runtime installed.

## Release policy

- Release only from an annotated `vMAJOR.MINOR.PATCH` tag.
- Do not publish when tests, installer smoke checks, vulnerability scanning,
  checksums, or provenance generation fail.
- Never commit signing certificates. GitHub secrets may supply an encrypted
  PFX and password; unsigned artifacts must be clearly identified as such.
- Signed artifacts use the clean base filename without a suffix. Never add
  `-signed`; only unsigned artifacts use `-unsigned`.
- Pin every Action by full commit SHA with its release in a trailing
  comment (`uses: owner/action@<sha> # vX.Y.Z`) so Dependabot can propose
  reviewed updates, and pin external binary downloads to both a version and
  SHA-256.
- Give each workflow job only the token permissions it uses. Only the
  release publish job may hold `contents: write`, `id-token: write` and
  `attestations: write`; the job that holds signing secrets reads only.
- Keep the WinFsp URL and SHA-256 in `build-installer.ps1`,
  `build-msi.ps1` and the `WINFSP_*` variables of `.github/workflows/ci.yml`
  synchronized. A mismatch must fail packaging and CI.
- The checked-in vendor tree is authoritative for release builds. Do not run
  an implicit dependency update during packaging.
- Dependabot may group and rebase routine minor/patch updates. rclone is
  intentionally excluded: update it manually only after reviewing commit
  chronology, the extracted mount adapter, the vendor delta, and
  `third_party/rclone/UPSTREAM.md`. A stable version number is not necessarily
  newer than the pinned pseudo-version commit.
- Update `CHANGELOG.md`, provenance records, screenshots, and user
  documentation before a stable release.

## Source changes

Do not hand-edit generated installer outputs or binaries. When changing
extracted rclone/WinFsp code, record the upstream commit and local delta in
the relevant `UPSTREAM.md`. Keep changes narrowly scoped so rebasing onto a
security update remains auditable.

The WinFsp runtime is distributed as its unmodified upstream MSI. Do not
unpack and re-author its driver components into the MapFTPDrive MSI: doing so
would compromise Windows Installer servicing and upstream signature
boundaries. The EXE installs it synchronously; the MSI embeds it and delegates
installation to the LocalSystem `install-dependencies` helper after the
parent transaction releases the installer mutex.
