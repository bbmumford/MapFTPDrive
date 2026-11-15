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

### Reproducing and auditing the extraction

From a checkout of rclone at the recorded commit, with `$MAPFTPDRIVE` set to
this repository:

```sh
extract() {
  sed -e '1s#^//go:build .*$#//go:build windows#' \
      -e 's#^package cmount$#package mount#' "$1"
}
extract cmd/cmount/fs.go                 > /tmp/cmount_fs_windows.go
extract cmd/cmount/mount.go              > /tmp/cmount_mount_windows.go
extract cmd/cmount/mountpoint_windows.go > /tmp/cmount_mountpoint_windows.go
for f in fs mount mountpoint; do
  diff -u /tmp/cmount_${f}_windows.go \
    "$MAPFTPDRIVE/internal/mount/cmount_${f}_windows.go"
done
```

The `diff` output is the complete local delta. It must contain nothing
except the local patches listed below. The same sources are available
without a git checkout in the Go module cache after
`go mod download github.com/rclone/rclone@v1.76.0`
(`go list -m -f '{{.Dir}}' github.com/rclone/rclone`).

When moving to a new rclone release:

1. confirm with `https://api.github.com/repos/rclone/rclone/compare/<old>...<new>`
   that the new commit is a descendant of the recorded one (`behind_by` is 0);
2. update `go.mod`, then run `go mod tidy` and `go mod vendor`;
3. re-extract the three files as above, review the upstream
   `cmd/cmount` and `cmd/mountlib` changes, and re-apply the local patches;
4. update this record.

## Local patches

None. The extracted files differ from upstream only by the two extraction
lines described above.

## License

The upstream `cmd/cmount` files carry no per-file copyright header. rclone is
licensed under the MIT license (`vendor/github.com/rclone/rclone/COPYING`),
which also covers the extracted adapter. MapFTPDrive as a whole is
distributed under GPLv3 (see `LICENSE`).
