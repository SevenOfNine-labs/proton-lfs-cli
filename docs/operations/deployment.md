# Deployment Guide

This guide is for development and CI environments. Production rollout is blocked by project plan gates.

## Prerequisites

- Go toolchain available.
- `git-lfs` installed and on `PATH`.
- Node.js 25+ for building `proton-drive-cli` SEA binary and running SDK tests.
- `pass-cli` for credential management.

No .NET SDK required.

## Local Bring-Up

```bash
git submodule update --init submodules/git-lfs submodules/pass-cli submodules/proton-drive-cli
git -C submodules/proton-drive-cli submodule update --init submodules/sdk
make check-submodules
make setup
make build-all    # Builds Go adapter, Git LFS, and proton-drive-cli
make test
make test-integration
```

`make check-submodules` avoids recursive descent into the Proton SDK because
the current upstream SDK commit contains nested gitlinks that are not declared
in its `.gitmodules` file.

Build proton-drive-cli:

```bash
make build-drive-cli
```

SDK-backed path:

```bash
pass-cli login
make test-integration-sdk
```

For accounts that need two-factor authentication, complete interactive
`proton-drive login` before SDK transfers. For two-password accounts, configure
a separate mailbox/data password credential provider instead of using secret
environment variables:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --credential-provider git-credential --data-credential-provider git-credential"
```

If `node` is not visible to non-interactive shells, pass an explicit binary path:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

Preflight only:

```bash
make check-sdk-prereqs
```

## Git LFS Agent Wiring

Repository-level configuration example:

```bash
git lfs install --local
git config lfs.customtransfer.proton.path "$(pwd)/bin/git-lfs-proton-adapter"
git config lfs.customtransfer.proton.args "--backend=local"
git config lfs.standalonetransferagent proton
```

Switch to SDK backend:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --drive-cli-bin=submodules/proton-drive-cli/dist/index.js"
```

## CI Notes

- Keep credentials in CI secret stores only.
- Prefer `PROTON_PASS_*` references and pass-cli in CI.
- Run `make test` and `make test-integration` on every PR.
