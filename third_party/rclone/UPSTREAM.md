# rclone source provenance

- Upstream: https://github.com/rclone/rclone
- Release: `v1.76.0`
- Commit: `e982ff36668f5130d94b4f70409babb3d964df42`
- Upstream description: `Version v1.76.0`
- Retrieved: 2026-11-15

The Go dependency closure used by MapFTPDrive is checked into `vendor/` at
this release (`github.com/rclone/rclone v1.76.0` in `go.mod`).

## Extracted mount adapter

The WinFsp/cgofuse adapter in `internal/mount` was extracted from
`cmd/cmount` at the commit above:

| Upstream file                     | Local file                                     |
| --------------------------------- | ---------------------------------------------- |
| `cmd/cmount/fs.go`                | `internal/mount/cmount_fs_windows.go`          |
| `cmd/cmount/mount.go`             | `internal/mount/cmount_mount_windows.go`       |
| `cmd/cmount/mountpoint_windows.go`| `internal/mount/cmount_mountpoint_windows.go`  |

The extraction changes exactly two lines in each file:

1. the first-line build constraint becomes `//go:build windows`; and
2. `package cmount` becomes `package mount`.

The other upstream `cmd/cmount` files (`arch.go`, `mount_brew.go`,
`mount_test.go`, `mount_unsupported.go`, `mountpoint_other.go`) serve other
platforms or rclone's own command tree and are not extracted.

After extraction, the local patches in `cmount-windows.patch` (beside this
file) are applied. Every patched place carries a `MapFTPDrive local patch:`
comment. The volume identity helpers the patches call live in
`internal/mount/cmount_volume.go`; that file and its tests are MapFTPDrive
code, not extracted.

### Reproducing and auditing the adapter

From a checkout of rclone at the recorded commit, with `$MAPFTPDRIVE` set to
this repository and `$WORK` an empty scratch directory:

```sh
extract() {
  sed -e '1s#^//go:build .*$#//go:build windows#' \
      -e 's#^package cmount$#package mount#' "$1"
}
mkdir -p "$WORK/internal/mount"
extract cmd/cmount/fs.go                 > "$WORK/internal/mount/cmount_fs_windows.go"
extract cmd/cmount/mount.go              > "$WORK/internal/mount/cmount_mount_windows.go"
extract cmd/cmount/mountpoint_windows.go > "$WORK/internal/mount/cmount_mountpoint_windows.go"
(cd "$WORK" && patch -p1 < "$MAPFTPDRIVE/third_party/rclone/cmount-windows.patch")
for f in fs mount mountpoint; do
  cmp "$WORK/internal/mount/cmount_${f}_windows.go" \
      "$MAPFTPDRIVE/internal/mount/cmount_${f}_windows.go"
done
```

Every `cmp` must succeed. Running `diff -u` between the extracted (unpatched)
files and the committed ones shows the complete local delta, which must
equal `cmount-windows.patch`. The same upstream sources are available
without a git checkout in the Go module cache after
`go mod download github.com/rclone/rclone@v1.76.0`
(`go list -m -f '{{.Dir}}' github.com/rclone/rclone`).

When moving to a new rclone release:

1. confirm with `https://api.github.com/repos/rclone/rclone/compare/<old>...<new>`
   that the new commit is a descendant of the recorded one (`behind_by` is 0);
2. update `go.mod`, then run `go mod tidy` and `go mod vendor`;
3. re-extract the three files as above, review the upstream `cmd/cmount` and
   `cmd/mountlib` changes, and apply `cmount-windows.patch`, resolving any
   rejected hunk by hand;
4. regenerate `cmount-windows.patch` from the extracted and committed files
   (`diff -ru a b` over two trees laid out as `internal/mount/...`) and update
   this record.

## Local patches

1. **Handle bounds** (`cmount_fs_windows.go`, `_getHandle`): the range check
   is `fh >= len(handles)`. Upstream's `>` let a handle equal to the table
   length index past the slice and panic inside a WinFsp callback, which
   ends the whole mount host.
2. **No rclone command or remote-control registration**
   (`cmount_mount_windows.go`): upstream's `init` registered a `mount` cobra
   command, the `mount/*` remote-control calls and a build tag. MapFTPDrive
   has neither an rclone command line nor a remote-control server; the
   engine calls `mount` directly through `mountlib.NewMountPoint`.
3. **Windows-only option and wait logic** (`cmount_mount_windows.go`,
   `cmount_fs_windows.go`): branches for Linux, macOS, FreeBSD and OpenBSD
   (FUSE `fsname`/`subtype`/`max_readahead`/`atomic_o_trunc`/
   `daemon_timeout`/`allow_*`/`ro`/AppleDouble options, the OpenBSD
   directory-offset quirk and the macOS FSKit xattr emulation) are removed,
   and the `runtime.GOOS == "windows"` waits run unconditionally. These files
   build only for Windows, so the removed code could never run.
4. **Unused symbols** (`cmount_mountpoint_windows.go`): `isDriveOrRootPath`
   and `isDriveOrRootPathRegex`, which nothing in the Windows build calls,
   are removed.
5. **Volume identity from the engine's options**
   (`cmount_mountpoint_windows.go` `handleVolumeName`/`getMountpoint`,
   `cmount_mount_windows.go` `mountOptions`, helpers in
   `cmount_volume.go`):
   - `VolumeName` is only the volume label, for disk and network volumes
     alike. It is passed unescaped by the engine; the adapter drops a
     character split by mountlib's 32-byte cut, escapes `\` and `,` for
     WinFsp's `-o` parser and passes `-o volname=`. A `volname=` entry in
     `ExtraOptions` replaces it.
   - A network volume needs exactly one `--VolumePrefix=\host\share` entry in
     `ExtraFlags` (a leading `\\` is shortened to `\`), or a UNC mount path.
     The adapter validates the shape and length WinFsp accepts and forces
     network mode when a prefix is present. Network mode without a prefix
     fails the mount.
   - Upstream instead reused `VolumeName` as the prefix and invented
     `\server\<VolumeName>` for plain names. mountlib cuts `VolumeName` to
     32 bytes before the adapter runs, which cannot carry an
     endpoint-unique prefix such as `\mapftpdrive\<endpoint ID>`, and a
     prefix built from a display name collides when two drives share it.

## License

The upstream `cmd/cmount` files carry no per-file copyright header. rclone is
licensed under the MIT license (`vendor/github.com/rclone/rclone/COPYING`),
which also covers the extracted adapter. MapFTPDrive as a whole is
distributed under GPLv3 (see `LICENSE`).
