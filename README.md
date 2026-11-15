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
- Open **Manage FTP Drives** from the Start menu (or run
  `mapftpdrive.exe manage`) to see every drive with its live status and to
  map, mount, unmount, edit, rename, remove, test or re-pin it.
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
- `setup.exe` for an interactive installation.
- `.msi` for Windows Installer and managed-deployment workflows.

Both formats install MapFTPDrive and the bundled WinFsp 2.1 runtime when it
is not already present. Installation requires administrator approval because
the WinFsp filesystem driver and the MapFTPDrive system service are
machine-wide components.

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

Unmount retains a disabled definition. Mount (or mapping the same server
to the same letter) enables it again. Remove deletes the definition, its
Credential Manager entry, its pinned host key, its copied private key and
its local cache; changes still waiting in the cache to be uploaded are
lost.

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
