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
- Right-click a managed drive and choose **Unmount FTP Drive**,
  **Rename FTP Drive** or **Remove FTP Drive**. Unmount keeps its
  definition; remove deletes it, its stored credential, its pinned server
  identity and its local cache.
- Open **MapFTPDrive** from the Start menu (or run
  `mapftpdrive.exe manage`) for the **Manage FTP Drives** window, which
  shows every drive with its live status and maps, mounts, unmounts, edits,
  renames, removes, tests or re-pins it.
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

- FTP, explicit FTPS, implicit FTPS, and SFTP endpoints, with the
  protocol's default port filled in (22 SFTP, 21 FTP and explicit FTPS,
  990 implicit FTPS).
- SFTP password or private-key login; the password or key passphrase is
  kept in Windows Credential Manager and the key file is copied into
  MapFTPDrive's own folder.
- Native mapping dialog that offers only free drive letters and refuses a
  letter already used on this computer or by another MapFTPDrive drive in
  either scope.
- A drive is saved only after a test connection and mount succeed, so a
  failed mapping never leaves a hidden definition holding the letter.
  Mapping onto the letter of an unmounted drive re-uses or replaces it.
- SFTP host keys are pinned on first use after the SHA-256 fingerprint is
  shown; an SFTP drive is never mounted without a pinned key. FTPS
  certificates that Windows does not trust can be pinned the same way.
  A changed host key or certificate stops the drive and shows the pinned
  and presented fingerprints before an explicit re-pin.
- Drives reconnect on their own: failed or dropped mounts are retried
  with increasing delays (about 5 s up to 5 min) and immediately when the
  network changes; the hosts wait for the network at boot and sign-in.
- Files written through a drive are cached locally first, so Office-style
  saves, random writes and upload retries work. Directory listings are
  cached for a minute, and network timeouts keep Explorer from hanging
  for long while a server is offline.
- Persistent per-user mounts restored at sign-in.
- UAC-protected system-wide mounts restored by a LocalSystem service.
- Explorer actions for map, mount, unmount, rename, and remove.
- Renaming uses **Rename FTP Drive** or the management window for every
  drive. WinFsp volumes do not accept Explorer's own Rename, so
  MapFTPDrive saves the new name and remounts the drive with it. Drive
  names are at most 32 characters and cannot contain `\`, `/` or commas.
- Logs: `%LOCALAPPDATA%\MapFTPDrive\logs` for your drives and the
  protected `%ProgramData%\MapFTPDrive\logs` plus the Windows Event Log
  (source MapFTPDrive) for system-wide drives.
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

1. Right-click **This PC** and select **Map FTP Drive**, or choose
   **Map new drive** in **Manage FTP Drives**.
2. Enter the drive name, protocol, server, port, username, password (or
   private key and passphrase for SFTP), remote path, and drive letter.
3. For SFTP, and for FTPS servers whose certificate Windows does not
   trust, compare the displayed SHA-256 fingerprint with one from the
   server's administrator before trusting it.
4. Use a managed drive's context menu or the management window to
   unmount, rename, or remove it.

Hold Shift while right-clicking **This PC** to reveal
**Map FTP Drive (system-wide)**. Windows requests UAC approval before the
system mapping dialog is displayed.

A system-wide drive is visible to every user of the computer, and all of
them can read, change and delete its remote files using the account the
administrator entered (the password itself stays hidden in the service's
Credential Manager). Map a per-user drive unless every local user may have
that access; see `SECURITY.md`.

Unmount retains a disabled definition. To mount it again, right-click
**This PC** and choose **Mount FTP Drive**, choose **Mount** in **Manage
FTP Drives**, or map the same server to the same letter. Remove deletes the
definition, its Credential Manager entry, its pinned host key, its copied
private key and its local cache; changes still waiting in the cache to be
uploaded are lost. The drive commands appear only on MapFTPDrive drives;
see `docs/architecture.md` for how Explorer recognises them.

## Uninstall

Uninstalling stops every signed-in user's MapFTPDrive host (their drives
unmount) and the system service. Unless you keep the data, it also removes:

- the `MapFTPDrive/...` entries in Credential Manager of every user signed
  in at that moment, and of the system service;
- each user's `%LOCALAPPDATA%\MapFTPDrive` (drive definitions, SFTP host
  keys, copied private keys, logs and caches), for every profile on the
  computer;
- `%ProgramData%\MapFTPDrive` (system drive definitions, host keys, copied
  private keys, the drive index, logs and caches).

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

The build uses the Go toolchain named by the `toolchain` line in `go.mod`
(currently Go 1.27.4); with the default `GOTOOLCHAIN=auto`, any installed Go
1.21 or newer downloads it automatically. Release builds use the checked-in
vendor tree:

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
