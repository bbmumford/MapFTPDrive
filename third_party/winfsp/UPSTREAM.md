# WinFsp source provenance

- Upstream: https://github.com/winfsp/winfsp
- Commit: `e3ecc89bb7c1356f9b08e55744bec875a1895306`
- Upstream description: `build: version 2026 Beta3`
- Retrieved: 2026-07-24

This subtree contains the active native interface headers, user-mode DLL,
shared code, kernel driver, and the Visual Studio projects required to build
those components. Documentation, samples, tests, .NET bindings, diagnostic
tools, and the upstream installer UI are intentionally excluded.

The unmodified upstream license and contributor notice are included beside
this file. Any local modifications must be documented here and in the
affected source file.

## Packaged runtime

Release installers embed the unmodified upstream WinFsp 2.1 redistributable:

- URL: `https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi`
- SHA-256: `073a70e00f77423e34bed98b86e600def93393ba5822204fac57a29324db9f7a`

The redistributable is downloaded and verified during packaging and is not
committed as a generated binary. Both the Inno Setup EXE and WiX MSI contain
it. The EXE invokes it directly; the MSI uses MapFTPDrive's asynchronous
LocalSystem dependency helper to avoid a nested Windows Installer
transaction. MapFTPDrive does not modify or remove the shared WinFsp
installation.
