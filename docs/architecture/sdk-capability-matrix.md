# SDK Capability Matrix

Date: 2026-02-10

This matrix defines which Proton SDK paths are realistically runnable by external contributors versus internal Proton environments.

## Runtime Paths In This Repository

| Path | Configuration | Needs Internal Proton Access | Uses Your Own Proton Account | Who Can Run It |
| --- | --- | --- | --- | --- |
| Local backend | `PROTON_LFS_BACKEND=local` | No | Not required (supports test credentials) | Anyone |
| SDK backend (proton-drive-cli) | `PROTON_LFS_BACKEND=sdk` | No | Yes | Anyone with Node.js 18+ |

## proton-drive-cli Bridge

The `proton-drive-cli` submodule (`submodules/proton-drive-cli/`) provides:

- Complete SRP authentication (no external SDK dependencies)
- E2E encryption via OpenPGP
- File upload/download with block-level encryption
- Session management and persistence
- Pure TypeScript — no .NET, no internal NuGet access required

Build: `make build-drive-cli`

## Policy For This Repository

1. Default contributor path is `PROTON_LFS_BACKEND=local`.
2. Real validation for external contributors uses `PROTON_LFS_BACKEND=sdk` with proton-drive-cli built locally.
3. No .NET SDK or internal NuGet access required for any path.
