# MapFTPDrive

MapFTPDrive is a native Windows application for mapping FTP, FTPS, and SFTP
endpoints as persistent drive letters. It embeds the selected rclone
backend/VFS code and hosts it directly on WinFsp; it does not invoke an
`rclone.exe` child process.

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
- `third_party`: exact vendored upstream source and notices used by the build.

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

Go 1.25 or newer is required. Release builds use the checked-in vendor tree:

```text
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor ./cmd/mapftpdrive
GOOS=windows GOARCH=arm64 go build -mod=vendor ./cmd/mapftpdrive
```

## Release artifacts

Each release produces x64 and ARM64 artifacts in two formats:

- The `setup.exe` bootstrapper includes the pinned, checksum-verified WinFsp
  runtime and is the recommended installation path.
- The `.msi` installs MapFTPDrive for managed deployment environments.
  WinFsp 2.1 or newer must already be deployed on the target computer.

Unsigned development artifacts include `-unsigned` in their filename.
Officially signed builds omit that suffix.

## License

MapFTPDrive and its combined rclone/WinFsp components are distributed under
GPLv3. Upstream copyright, license, and provenance records are retained in
`vendor` and `third_party`.
