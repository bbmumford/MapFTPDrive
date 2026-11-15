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

Download the installer for the computer's architecture from the latest
GitHub release:

- `windows-x64` for ordinary 64-bit Intel/AMD Windows computers.
- `windows-arm64` for Windows on ARM.
- `-setup.exe` for an interactive installation (recommended).
- `-bundle-setup.exe` for managed deployment of the Windows Installer
  packages with WinFsp included.
- `.msi` for administrators who deploy WinFsp 2.1 or later themselves.

Both setup programs install the bundled WinFsp 2.1 runtime when no equal or
newer WinFsp is present, and keep a newer one. Installation requires
administrator approval because the WinFsp filesystem driver and the
MapFTPDrive system service are machine-wide components.

After installation:

1. Right-click **This PC** and select **Map FTP Drive**.
2. Enter the server, protocol, port, username, password, remote path, drive
   label, and drive letter.
3. For SFTP, verify the displayed host-key fingerprint.
4. Use a managed drive's context menu to unmount, rename, or remove it.

Hold Shift while right-clicking **This PC** to reveal
**Map FTP Drive (system-wide)**. Windows requests UAC approval before the
system mapping dialog is displayed.

Unmount retains a disabled definition. Right-click **This PC** and choose
**Mount FTP Drive** to mount it again. Remove deletes both the definition
and its Credential Manager entry. The drive commands appear only on
MapFTPDrive drives; see `docs/architecture.md` for how Explorer recognises
them.

## Uninstall

Uninstalling stops every signed-in user's MapFTPDrive host (their drives
unmount) and the system service. Unless you keep the data, it also removes:

- the `MapFTPDrive/...` entries in Credential Manager of every user signed
  in at that moment, and of the system service;
- each user's `%LOCALAPPDATA%\MapFTPDrive` (drive definitions, SFTP host
  keys, logs and caches), for every profile on the computer;
- `%ProgramData%\MapFTPDrive` (system drive definitions, host keys, the drive
  index and logs).

How to keep the data for a later reinstall:

- `setup.exe` installs: answer **No** when the uninstaller asks, or run the
  uninstaller with `/KEEPDATA` (`unins000.exe /SILENT /KEEPDATA`). A silent
  uninstall without `/KEEPDATA` removes the data.
- MSI and setup-bundle installs: `msiexec /x <package or product code>
  KEEPDATA=1`. Upgrades always keep the data.

Windows encrypts each user's Credential Manager with that user's sign-in, so
the passwords of users who are not signed in during the uninstall cannot be
removed. Those users can delete the `MapFTPDrive/endpoint/...` entries under
**Credential Manager > Windows Credentials > Generic Credentials**, or find
them with `cmdkey /list` and remove each with
`cmdkey /delete:MapFTPDrive/endpoint/<id>`.

WinFsp stays installed; remove it separately from **Settings > Apps** if no
other program needs it.

## Build

Go 1.25 or newer is required. Release builds use the checked-in vendor tree:

```text
go test -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor ./cmd/mapftpdrive
GOOS=windows GOARCH=arm64 go build -mod=vendor ./cmd/mapftpdrive
```

The executable's application manifest (Common Controls 6, per-monitor v2
DPI awareness, `asInvoker`, Windows 10/11), icon and version information
come from `cmd/mapftpdrive/winres` and are linked from the committed
`cmd/mapftpdrive/rsrc_windows_*.syso` files. After changing that directory,
run `scripts/generate-resources.ps1` (PowerShell 7 on any OS; it runs the
pinned `go-winres` through `go run`) and commit the regenerated files.
`build-installer.ps1` regenerates them with the release version.

## Release artifacts

Each release produces x64 and ARM64 artifacts in three forms:

- `MapFTPDrive-<version>-windows-<arch>-setup.exe` (Inno Setup) carries the
  pinned, checksum-verified WinFsp MSI and is the recommended installation
  path. Before copying files it reads the installed WinFsp version
  (`HKLM\SOFTWARE\WinFsp\Version`, 32-bit view); it skips WinFsp when the
  same or a newer version is present, otherwise runs the WinFsp MSI and
  handles its result: 3010 continues and asks for a restart at the end, 1638
  keeps the installed version, 1618 (another installation running), 1602
  (cancelled) and 1603 stop Setup with a message; the WinFsp log is
  `%TEMP%\MapFTPDrive-WinFsp-install.log`.
- `MapFTPDrive-<version>-windows-<arch>-bundle-setup.exe` is a WiX Burn
  bundle that chains the same WinFsp MSI and the MapFTPDrive MSI as separate
  transactions. It skips WinFsp when the same or a newer release of its
  upgrade family is installed, stops with a message when an older WinFsp
  that cannot be upgraded is present, and never removes WinFsp. Use it for
  Intune, Configuration Manager or Group Policy deployment of MSI-based
  packages: `bundle-setup.exe /quiet /norestart`. Exit code 0 is success,
  3010 means a restart is required to finish, and other codes are the
  failing package's Windows Installer error; `/log <file>` writes the
  bundle and package logs.
- `MapFTPDrive-<version>-windows-<arch>.msi` contains MapFTPDrive alone. It
  refuses to install unless WinFsp is present, so deploy WinFsp 2.1 or
  later (the upstream MSI from https://winfsp.dev) first, for example as an
  Intune dependency.

Unsigned development artifacts include `-unsigned` in their filename.
Signed builds use the clean base filename and never add `-signed`.
Every artifact has a sibling `.sha256` file, and each release includes a
combined `SHA256SUMS` manifest and GitHub build-provenance attestations.

Uninstalling MapFTPDrive does not remove WinFsp because other filesystem
applications may depend on it.

## License

MapFTPDrive and its combined rclone/WinFsp components are distributed under
GPLv3. Upstream copyright, license, and provenance records are retained in
`vendor` and `third_party`.
