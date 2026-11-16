# Architecture

## One engine, two supervisors

The executable has a single in-process mount engine. A per-user supervisor
starts at sign-in and restores user-scoped drives. The Windows service uses
the same engine to restore system-scoped drives as LocalSystem.

The engine constructs an rclone FTP or SFTP filesystem directly, layers the
rclone VFS over it, and exposes that VFS through the cgofuse/WinFsp host.
FTPS uses the FTP backend with explicit or implicit TLS. No external
rclone process, configuration file, or remote-control API is involved.

The VFS caches written files on disk (cache mode `writes`), caches
directory listings for a minute, and rclone's connect (10 s) and idle
(60 s) timeouts with two low-level attempts bound how long Explorer waits
on an unreachable server. SFTP host keys are pinned through rclone's
`host_keys` option or a known_hosts file, and remote shell commands are
disabled. rclone's FTP backend cannot pin certificates, so a check on
rclone's shared TLS configuration enforces a pinned certificate's SHA-256
on every FTPS handshake, and rejects any handshake that skipped normal
verification without a matching pin. rclone's log output goes to the
host's log.

WinFsp remains a separately serviced Windows filesystem driver rather than
being linked into the Go executable. It is delivered inside every
MapFTPDrive installer, so users do not have to locate or install it
separately.

## Service lifecycle

The `MapFTPDrive` service runs `MapFTPDrive.exe service` as LocalSystem with
delayed automatic start, so system drives connect after the network is up.
Its recovery actions restart it 5, 15 and 60 seconds after a crash or an
error exit, and the failure count resets after one day. Both installers
apply these settings with the same code: the EXE's `install` step creates
the service, or stops an existing one, rewrites its configuration and
restarts it, so an upgrade runs the new executable with current settings;
the MSI creates it with `ServiceInstall` and runs `configure-service` as a
custom action before starting it. The EXE stops the service before
replacing files.

## Elevation

Both installers register every Explorer verb machine-wide under
`HKLM\Software\Classes` and one startup entry under HKLM's `Run` key; the
program itself never writes verbs into a user's hive. A per-user copy of
these keys would shadow the machine-wide verbs for that user, so the EXE's
`install` and `uninstall` steps delete any copy in the installing account's
hive. The normal verb opens the mapper unelevated. The extended verb has
the `Extended` and `HasLUAShield` registry values and relaunches the
system-scope operation with `runas`. The elevated
process sends the definition and a protected credential to the installed
service. Merely holding Shift does not silently grant system scope; Windows
still displays its UAC consent UI.

## Control pipes

Mapping commands reach the hosts over named pipes:

- `\\.\pipe\MapFTPDrive-system`, served by the LocalSystem service, with a
  DACL admitting SYSTEM and Administrators (an elevated token).
- `\\.\pipe\MapFTPDrive-user-<sid-hash>-<session>`, served by the user's
  host in that logon session, admitting SYSTEM and that user. The session
  number keeps one account signed in twice (console and Remote Desktop) on
  separate hosts, each owning the drives of its own session.

Pipe names are predictable and the first process to create a name serves
it, so before writing a request (which may carry a password) the client
asks Windows which process serves the pipe (`GetNamedPipeServerProcessId`)
and reads that process's token. The system pipe must be served by
`S-1-5-18`; a user pipe by the caller's own SID in the caller's session.
Anything else fails closed with "the MapFTPDrive control pipe is served by
an untrusted process" and no request is sent. Clients connect at the
anonymous impersonation level, so a server can never impersonate them.

Each request is bounded by `control.RequestTimeout` (two minutes) on both
sides. The host answers `accepted` as soon as it has read a valid request
and `working` every five seconds while a connect or mount is in progress;
the client gives up if fifteen seconds pass without any message. The final
message carries the result: an error, an informational message (such as a
mount still connecting in the background), the saved definition, or the
status report. The host's request handler stops fifteen seconds before
`RequestTimeout`, so its own answer, not a timeout, reaches the client. A
`shutdown` request, which the uninstaller sends to every user host, makes
the host unmount its drives and exit.

## Supervision

Each scope has one host: the per-user host (`daemon --scope user`, started
at sign-in) and the LocalSystem service. A host takes its scope's control
pipe before it mounts anything; the pipe is created as the first instance,
so it doubles as a singleton lock and a second host for the same scope
exits without touching any drive. Clients (`ensureHost`) ping the pipe,
start the host only when nothing answers, and wait up to 60 seconds for it
to answer instead of guessing start-up delays; a pipe that fails the
identity check ends the wait at once, because a new host could not take
its name.

After taking the pipe the host lists every stored drive, waits up to 90
seconds for a usable network, and then supervises each enabled drive in
its own loop:

- a failed attempt is retried after about 5 s, doubling to 5 minutes, with
  20% jitter; a network change (IP interface or address notification)
  retries waiting drives at once;
- a mount that ends without a request is remounted, never deleted;
- a changed or unpinned server identity, a missing credential or a missing
  key file stops retrying until the user acts (mount, edit or re-pin).

User operations on one drive are serialised, and the loop alone owns the
mount, so mount and unmount cannot interleave. The `status` request
reports each drive as mounted, connecting, retrying (with the next attempt
time and last error), unmounted, or needing attention (with the pinned and
presented fingerprints for an identity change).

Map tests the server identity, mounts with the supplied secret and only
then stores the credential and definition, undoing every step on failure.
Edit does the same and restores the previous mount when the new
definition fails. Rename remounts a mounted drive with the new label,
because WinFsp FUSE volumes reject Explorer's label changes.

## Persistence

User definitions live under `%LOCALAPPDATA%\MapFTPDrive`. System definitions
live under `%ProgramData%\MapFTPDrive`. Passwords and private-key
passphrases are separate Windows Credential Manager entries.

`endpoints.json` is a versioned envelope (`{"version": 1, "endpoints":
[...]}`); the unversioned array written by earlier builds is read and
rewritten on the next save, and a newer version is refused rather than
overwritten. Endpoint IDs are UUIDs and every per-drive file is named by a
hash of the ID. Entries that fail validation are skipped, reported and
written back unchanged. Saves go through a temporary file that is flushed
before a write-through rename, and the previous file is kept as
`endpoints.json.bak`, which is used if the main file cannot be read; a
main file that cannot be decoded is set aside as `endpoints.json.corrupt`
by the next save instead of replacing the backup.

Beside the store each scope keeps `known_hosts\<hash>` (pinned SFTP host
keys, when the linked rclone has no `host_keys` option), `keys\<hash>`
(copied private keys), `cache\vfs\<hash>` and `cache\vfsMeta\<hash>` (the
VFS write cache) and `logs` (rotating `mapftpdrive.log`).

`%ProgramData%` lets every user create files and folders, so the system
directory carries its own protected descriptor
(`O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`): owned by Administrators,
nothing inherited from `%ProgramData%`, full control for SYSTEM and
Administrators only. Anyone else who could write it could change the
endpoints the service mounts with stored passwords, or redirect the
service's file writes.

- Both installers run `MapFTPDrive.exe prepare-system-data` (the MSI as a
  LocalSystem custom action, the EXE through `install`). It creates the
  directory with that descriptor, or repairs an existing one: a junction or
  symbolic link at that path is removed rather than followed, the directory
  is locked first, and every entry another principal could have written
  (owned by someone else, writable by someone else, a reparse point, or a
  file with extra hard links) is deleted before the descriptor is
  propagated to what remains.
- The endpoint store creates the directory with the same descriptor if it
  is ever missing, instead of a plain `MkdirAll`.
- The service checks the directory and its direct entries at every start.
  If any is a reparse point, is owned by anyone other than SYSTEM,
  Administrators or TrustedInstaller, or grants write access to anyone
  else, the service stops with service-specific exit code 3 and writes
  event ID 1 from source `MapFTPDrive` to the Application log. Repairing or
  reinstalling restores the directory.

Unelevated processes, such as the Explorer verbs, the mapper and user
hosts, cannot read the system store. The system store therefore keeps
`drives.json` beside it: drive letter, scope, ID, name and whether the
drive is unmounted, with no server, user name, path, host key or credential
reference. It is rewritten after every system store save and whenever the
service's host starts, and is created with its own descriptor
(`O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)`) before it replaces
the previous index, so Users may read but not change it. The Explorer
drive commands use it to recognise system drives, and the mapper and user
hosts use it to refuse a letter a system drive holds. Readers re-validate
every entry. Elevated processes read the store itself.

Unmount transitions a definition to inactive but retains it. Remove
unmounts it, removes the definition, and deletes its credential, pinned
host key, copied key and cache.

## Explorer integration

Static verbs are registered against the `CLSID_MyComputer` shell object for
mapping (**Map FTP Drive**, **Map FTP Drive (system-wide)**) and for
remounting a drive that **Unmount** disabled (**Mount FTP Drive**, which
lists the unmounted managed drives: an unmounted drive has no drive item to
right-click). The system-wide mapping verb is marked `Extended`, so it
appears only in the extended (Shift) menu on classic Windows Explorer.

Managed drive verbs (**Unmount**, **Rename**, **Remove**) are registered on
`Drive\shell` with an `AppliesTo` condition:

```text
System.Volume.FileSystem:="FUSE-MapFTP"
```

Every MapFTPDrive mount passes `--FileSystemName=MapFTP` to WinFsp, which
reports the volume's file system as `FUSE-MapFTP` (WinFsp adds the `FUSE-`
prefix and keeps at most 15 characters). Explorer evaluates the condition per
drive, so the verbs never appear on C:, removable media, SMB shares or other
FUSE file systems. The condition is a display filter only: each command
still resolves the selected drive against the managed endpoint stores and
refuses anything else.

Explorer expands `%V` for a drive to its root, such as `X:\`. The verbs pass
`--drive "%V."`: quoting `"%V"` alone would let the root's trailing
backslash escape the closing quote, and the program would receive `X:"`.
The drive argument is normalised to a single letter (`X`, `X:`, `X:\`,
`X:\.` and the `X:"` that a quoted `"%V"` produces are accepted; paths are
rejected).
`internal/explorer/verbs_test.go` parses the exact command strings from the
Go verb table and both installer sources with the argv rules Go uses on
Windows.

Explorer starts these commands without a console. Errors from map, mount,
unmount, rename and remove are therefore shown in a message box; installer
and service commands report failure only through their exit code.

## Installation and dependencies

The Inno Setup EXE and the WiX Burn bundle both carry the same pinned
WinFsp 2.1 redistributable, the unmodified upstream MSI. The build
downloads that exact file and verifies its SHA-256 before packaging; Burn
also records the payload hash and verifies it before running it.

A Windows Installer transaction cannot run another MSI, so WinFsp is never
installed from inside the MapFTPDrive MSI:

- The EXE installs WinFsp from `PrepareToInstall`, before any MapFTPDrive
  file is copied. It reads the installed version from the 32-bit
  `HKLM\SOFTWARE\WinFsp` `Version` value, skips WinFsp when the same or a
  newer version is present (the WinFsp MSI refuses downgrades), and maps
  `msiexec` results: 0 continues; 3010 continues and requests a restart at
  the end; 1638 keeps the installed version; 1618, 1602 and 1603 stop Setup
  with a message naming the WinFsp log.
- The Burn bundle (`installer/MapFTPDrive.bundle.wxs`) chains the WinFsp MSI
  and the MapFTPDrive MSI as separate transactions run by the Burn engine,
  which also handles restart results (3010) and the installer mutex. A
  product search on WinFsp's upgrade code skips the WinFsp package when an
  equal or newer release is installed, and a launch condition stops before
  any change when an older WinFsp that its MSI cannot upgrade is present.
  The WinFsp package is `Permanent`, so removing the bundle leaves it.
- The plain MapFTPDrive MSI carries no WinFsp. A launch condition refuses
  installation while the `HKLM\SOFTWARE\WinFsp` `InstallDir` value is
  missing, for administrators who deploy WinFsp themselves.

MapFTPDrive uninstall never removes the shared WinFsp runtime.

Removal runs `internal/cleanup` as LocalSystem, the only account that can
open every user's host pipe and its own credential vault: the MSI as a
deferred custom action (`uninstall-cleanup`) after the service stops and
before files are removed; the EXE's `uninstall` step through the service,
whose command line it points at `service --cleanup` for one run. Cleanup
sends `shutdown` to every `MapFTPDrive-user-*` pipe, waits for those hosts
to exit and terminates any that remain (only processes still running
`MapFTPDrive.exe`). Unless the data is kept, it then runs `purge-user` as
the user of every signed-in session (only that user's logon can open their
Credential Manager), deletes `AppData\Local\MapFTPDrive` from every profile
without following links, deletes LocalSystem's `MapFTPDrive/*` credentials
and removes `%ProgramData%\MapFTPDrive`. Upgrades only stop the hosts.

Release packaging builds x64 and ARM64 setup EXEs, setup bundles and MSIs.
Authenticode signing is enabled only when both signing secrets are present,
or when a certificate thumbprint names a certificate already in the Windows
store; signtool selects the certificate by thumbprint, never by a password
on its command line (see `CONTRIBUTING.md`). The bundle's Burn engine is
detached, signed and reattached before the bundle itself is signed.
Unsigned artifacts receive `-unsigned`; signed artifacts receive no suffix.
SHA-256 files, a combined manifest, and provenance attestations accompany
releases. A release is published only after the CI checks and an
install/uninstall smoke test of each architecture's MSI pass.

## Upstream source

The Go dependency closure needed for FTP, SFTP, VFS, and cgofuse is retained
in `vendor`. The installer carries the unmodified, hash-pinned upstream
WinFsp 2.1 redistributable, whose corresponding source is the upstream
`v2.1` tag. `third_party/winfsp` holds WinFsp's unmodified license and a
later, unbuilt reference snapshot of its source; its `UPSTREAM.md` records
both commits. Because the application combines
GPL-covered components, the project and distributed binaries use a
GPL-compatible distribution model and retain corresponding source and
notices.
