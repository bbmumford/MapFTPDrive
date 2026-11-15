# Contributing

Read `AGENTS.md` before changing code. Keep changes focused, add tests for
lifecycle and persistence behavior, preserve upstream provenance, and run:

```text
gofmt -w cmd internal
go test -mod=vendor ./...
go vet -mod=vendor ./...
GOOS=windows GOARCH=amd64 go build -mod=vendor ./...
GOOS=windows GOARCH=arm64 go build -mod=vendor ./...
```

Never place passwords, private keys, tokens, signing material, or real server
details in source, fixtures, issues, or logs.

Installer changes must preserve the all-in-one installation contract for
both `setup.exe` and `.msi`. The pinned WinFsp URL and SHA-256 currently
appear in both packaging scripts; update and review them together. Never
replace the hash check with a latest-release download.

Before merging installer work, run the release workflow as a branch
`workflow_dispatch` dry run for x64 and ARM64. Smoke-test the resulting EXE
and MSI on a clean Windows machine without WinFsp, plus a machine with
WinFsp already installed. Exercise install, upgrade, repair, uninstall, and
reboot/sign-in restoration. The MSI dependency helper is intentionally
asynchronous because Windows Installer forbids nested MSI transactions.

Do not commit files from `build/` or `installer/output/`. Signed artifact
filenames use no signing suffix; unsigned artifacts must end in `-unsigned`
before the extension.

Dependabot rebases open updates automatically and groups compatible
minor/patch updates. rclone updates arrive as their own grouped pull request
because MapFTPDrive maintains extracted adapter code and provenance. Before
merging one, confirm the new commit descends from the recorded one,
regenerate `vendor`, re-extract and re-patch the adapter, review the full
dependency delta, and update `third_party/rclone/UPSTREAM.md`.
