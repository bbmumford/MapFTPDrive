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

## Elevation

The normal Explorer verb is registered under HKCU and opens the mapper
unelevated. The extended verb has the `Extended` and `HasLUAShield` registry
values and relaunches the system-scope operation with `runas`. The elevated
process sends the definition and a protected credential to the installed
service. Merely holding Shift does not silently grant system scope; Windows
still displays its UAC consent UI.

## Supervision

Each scope has one host: the per-user host (`daemon --scope user`, started
at sign-in) and the LocalSystem service. A host takes its scope's control
pipe before it mounts anything; the pipe is created as the first instance,
so it doubles as a singleton lock and a second host for the same scope
exits without touching any drive. Clients (`ensureHost`) wait for the pipe
to answer instead of guessing start-up delays.

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
live under `%ProgramData%\MapFTPDrive` with an ACL limited to Administrators
and SYSTEM. Passwords and private-key passphrases are separate Windows
Credential Manager entries.

`endpoints.json` is a versioned envelope (`{"version": 1, "endpoints":
[...]}`); the unversioned array written by earlier builds is read and
rewritten on the next save, and a newer version is refused rather than
overwritten. Endpoint IDs are UUIDs and every per-drive file is named by a
hash of the ID. Entries that fail validation are skipped, reported and
written back unchanged. Saves go through a temporary file that is flushed
before a write-through rename, and the previous file is kept as
`endpoints.json.bak`, which is used if the main file cannot be read.

Beside the store each scope keeps `known_hosts\<hash>` (pinned SFTP host
keys, when the linked rclone has no `host_keys` option), `keys\<hash>`
(copied private keys), `cache\vfs\<hash>` and `cache\vfsMeta\<hash>` (the
VFS write cache) and `logs` (rotating `mapftpdrive.log`). The system host
also writes `drives.json`, a secret-free index of drive letter, scope, ID
and name with a DACL that lets Users read it, so unelevated processes can
recognise system drives and avoid their letters.

Unmount transitions a definition to inactive but retains it. Remove
unmounts it, removes the definition, and deletes its credential, pinned
host key, copied key and cache.

## Explorer integration

Static verbs are registered against the `CLSID_MyComputer` shell object for
mapping. Managed drive verbs are registered with an application command
that validates the selected drive against the endpoint store before acting.
The system-wide mapping verb is marked `Extended`, so it appears only in the
extended (Shift) menu on classic Windows Explorer.

## Installation and dependencies

The Inno Setup EXE and WiX MSI both embed the same pinned WinFsp 2.1
redistributable. The build downloads that exact upstream MSI and verifies
its SHA-256 before packaging.

The EXE runs WinFsp synchronously before registering MapFTPDrive. An MSI
cannot start another MSI while its own transaction owns the global Windows
Installer mutex. The MapFTPDrive MSI therefore installs the WinFsp payload
alongside the application and starts `install-dependencies` as LocalSystem
without waiting. That helper:

1. checks the 32-bit `HKLM\Software\WinFsp` installation entry;
2. returns immediately when WinFsp is already installed;
3. invokes the embedded MSI silently when it is absent; and
4. retries Windows Installer error 1618 until the parent transaction exits.

The runtime payload remains owned by the MapFTPDrive MSI for repair and
upgrade integrity. MapFTPDrive uninstall does not remove the shared WinFsp
runtime.

Release packaging builds x64 and ARM64 setup EXEs and MSIs. Authenticode
signing is enabled only when both signing secrets are present. Unsigned
artifacts receive `-unsigned`; signed artifacts receive no suffix. SHA-256
files, a combined manifest, and provenance attestations accompany releases.

## Upstream source

The Go dependency closure needed for FTP, SFTP, VFS, and cgofuse is retained
in `vendor`. Selected WinFsp source/provenance and its unmodified license are
retained in `third_party/winfsp`; the installer carries the unmodified,
hash-pinned upstream WinFsp redistributable. Because the application combines
GPL-covered components, the project and distributed binaries use a
GPL-compatible distribution model and retain corresponding source and
notices.
