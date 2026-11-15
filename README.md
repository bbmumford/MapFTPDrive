# MapFTPDrive

MapFTPDrive is a native Windows application for mapping FTP, FTPS, and SFTP
endpoints as persistent drive letters. It embeds the selected rclone
backend/VFS code and hosts it directly on WinFsp; it does not invoke an
`rclone.exe` child process. Both supported installers carry the WinFsp
runtime, so a separate prerequisite download is not required.

## Intended Windows experience

- Right-click **This PC** and choose **Map FTP Drive** for a persistent
  per-user drive.
- Shift-right-click **This PC** and choose **Map FTP Drive (system-wide)**.
  Windows requests elevation and installs the mount under a LocalSystem
  service, making it available to all users.
- Right-click a managed drive and choose **Unmount FTP Drive** or
  **Remove FTP Drive**. Unmount keeps its definition; remove deletes it and
  its stored credential.
- Secrets are held in Windows Credential Manager, never in the JSON endpoint
  store or command-line arguments.

## Source layout

- `cmd/mapftpdrive`: GUI, command dispatcher, and service entry point.
- `internal/config`: shared endpoint model and persistent configuration.
- `internal/credentials`: Windows Credential Manager integration.
- `internal/explorer`: Explorer verb registration.
- `internal/mount`: embedded rclone VFS and WinFsp host.
- `internal/service`: per-user startup and LocalSystem service supervisors.
- `third_party`: upstream provenance records, the mount adapter's local
  patch, and a reference WinFsp source snapshot that is not built (see
  `third_party/winfsp/UPSTREAM.md`).

See `docs/architecture.md` for component and security boundaries.

## Features

- FTP, explicit FTPS, implicit FTPS, and SFTP endpoints.
- Native Windows mapping dialog with automatic free-drive selection.
- SFTP trust-on-first-use prompt with a pinned SHA-256 host key.
- Credentials stored in Windows Credential Manager.
- Persistent per-user mounts restored at sign-in.
- UAC-protected system-wide mounts restored by a LocalSystem service.
- Explorer actions for map, mount, unmount, rename, and remove.
- Explorer's ordinary Rename action is monitored and persisted for user
  drives. System-wide network volumes use **Rename FTP Drive** and a
  controlled remount.
- Reproducible vendored Go dependencies and auditable upstream provenance.

## Use

Download the installer for the computer's architecture from the latest
GitHub release:

- `windows-x64` for ordinary 64-bit Intel/AMD Windows computers.
- `windows-arm64` for Windows on ARM.
- `setup.exe` for an interactive installation.
- `.msi` for Windows Installer and managed-deployment workflows.

Both formats install MapFTPDrive and the bundled WinFsp 2.1 runtime when it
is not already present. Installation requires administrator approval because
the WinFsp filesystem driver and the MapFTPDrive system service are
machine-wide components.

After installation:

1. Right-click **This PC** and select **Map FTP Drive**.
2. Enter the server, protocol, port, username, password, remote path, drive
   label, and drive letter.
3. For SFTP, verify the displayed host-key fingerprint.
4. Use a managed drive's context menu to unmount, rename, or remove it.

Hold Shift while right-clicking **This PC** to reveal
**Map FTP Drive (system-wide)**. Windows requests UAC approval before the
system mapping dialog is displayed.

Unmount retains a disabled definition. Map/mount enables it again. Remove
deletes both the definition and its Credential Manager entry.

## Build

The build uses the Go toolchain named by the `toolchain` line in `go.mod`
(currently Go 1.27.4); with the default `GOTOOLCHAIN=auto`, any installed Go
1.21 or newer downloads it automatically. Release builds use the checked-in
vendor tree:

```text
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor ./cmd/mapftpdrive
GOOS=windows GOARCH=arm64 go build -mod=vendor ./cmd/mapftpdrive
```

## Release artifacts

Each release produces x64 and ARM64 artifacts in two formats:

- The `setup.exe` bootstrapper includes the pinned, checksum-verified WinFsp
  runtime and is the recommended installation path.
- The `.msi` also embeds the same WinFsp runtime for Windows Installer and
  managed deployment environments. If WinFsp is absent, an elevated
  post-install helper waits for the MapFTPDrive MSI transaction to finish
  and then installs WinFsp silently. This avoids unsupported nested MSI
  transactions.

Unsigned development artifacts include `-unsigned` in their filename.
Signed builds use the clean base filename and never add `-signed`.
Every artifact has a sibling `.sha256` file, and each release includes a
combined `SHA256SUMS` manifest and GitHub build-provenance attestations.

The MSI dependency helper detects WinFsp through its machine installation
registry entry and skips installation when it is already present.
Uninstalling MapFTPDrive does not remove WinFsp because other filesystem
applications may depend on it.

## License

MapFTPDrive and its combined rclone/WinFsp components are distributed under
GPLv3. Upstream copyright, license, and provenance records are retained in
`vendor` and `third_party`.
