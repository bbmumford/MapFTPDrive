# WinFsp source provenance

MapFTPDrive ships the unmodified upstream WinFsp 2.1 runtime. The source
subtree in this directory is a separate, later development snapshot kept as
a reference. Nothing in this directory is compiled, linked or packaged.

## Packaged runtime

Release installers embed the unmodified upstream WinFsp 2.1 redistributable:

- Release: WinFsp 2025 (`v2.1`), the newest stable WinFsp release
- Commit: `ddca7bd5481857a65ba552f643b8776fd070836f`
- Corresponding source: https://github.com/winfsp/winfsp/tree/v2.1
- URL: `https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi`
- SHA-256: `073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a`

The redistributable is downloaded and verified during packaging and is not
committed as a generated binary. Both the Inno Setup EXE and WiX MSI contain
it. The EXE invokes it directly; the MSI uses MapFTPDrive's asynchronous
LocalSystem dependency helper to avoid a nested Windows Installer
transaction. MapFTPDrive does not modify or remove the shared WinFsp
installation. The same URL and SHA-256 are pinned in
`scripts/build-installer.ps1`, `scripts/build-msi.ps1` and
`.github/workflows/ci.yml`, which installs the runtime's Developer headers
for the race-detector build.

MapFTPDrive talks to the runtime only through its FUSE compatibility layer:
cgofuse loads the installed `winfsp-*.dll` at run time and passes the
WinFsp FUSE options the mount adapter builds (`-o volname=`,
`--VolumePrefix=`, `--FileSystemName=`, `-o uid=`/`gid=`). Those options and
the escaped-comma `-o` parsing they rely on are present in v2.1
(`src/dll/fuse/fuse.c`, `src/dll/fuse/fuse_opt.c`).

## Reference source snapshot

- Upstream: https://github.com/winfsp/winfsp
- Commit: `e3ecc89bb7c1356f9b08e55744bec875a1895306`
- Upstream description: `build: version 2026 Beta3` (pre-release `v2.2B3`)
- Relation to the packaged runtime: 42 commits after `v2.1`
- Retrieved: 2026-07-24

This subtree contains the native interface headers, user-mode DLL, shared
code, kernel driver, and the Visual Studio projects required to build those
components. Documentation, samples, tests, .NET bindings, diagnostic tools,
and the upstream installer UI are intentionally excluded.

It is not the corresponding source of the shipped binary and is not built by
any script or workflow: it serves as a readable copy of the FUSE layer and
driver interfaces (for example the volume prefix checks in
`src/sys/volume.c` and option parsing in `src/dll/fuse`) that the mount
adapter targets. Behaviour that MapFTPDrive depends on must be confirmed
against `v2.1`, not only against this snapshot. When the packaged runtime
moves to a newer stable release, replace this subtree with that release's
source so the two match again.

The unmodified upstream license and contributor notice are included beside
this file. Any local modifications must be documented here and in the
affected source file.
