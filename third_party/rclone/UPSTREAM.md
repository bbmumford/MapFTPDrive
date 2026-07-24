# rclone source provenance

- Upstream: https://github.com/rclone/rclone
- Commit: `a110fa5d47e3c47a13ca73b48666c48088d8263b`
- Upstream description: `build: update google.golang.org/grpc to fix multiple security problems`
- Retrieved: 2026-07-24

The Go dependency closure used by MapFTPDrive is checked into `vendor/`.
The WinFsp/cgofuse adapter in `internal/mount/cmount_*_windows.go` was
extracted from `cmd/cmount` at this same commit and changed only to:

1. use the local `mount` package name;
2. build specifically for Windows; and
3. expose it through MapFTPDrive's engine interface.

The original copyright headers remain in the extracted files. rclone is
licensed under GPLv3; its vendored license and notices are retained in the
dependency source tree.

