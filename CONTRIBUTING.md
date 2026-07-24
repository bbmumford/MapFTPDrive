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

