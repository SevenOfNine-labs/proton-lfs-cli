# Proton LFS CLI

End-to-end encrypted Git LFS backend for Proton Drive.

[![Documentation](https://img.shields.io/badge/docs-unified-blue)](https://sevenofnine-labs.github.io/proton-lfs-cli/) [![Go Reference](https://pkg.go.dev/badge/github.com/SevenOfNine-labs/proton-lfs-cli.svg)](https://pkg.go.dev/github.com/SevenOfNine-labs/proton-lfs-cli) [![Tests](https://github.com/SevenOfNine-labs/proton-lfs-cli/actions/workflows/test.yml/badge.svg)](https://github.com/SevenOfNine-labs/proton-lfs-cli/actions/workflows/test.yml)

## Current State (2026-02-10)

- Git LFS custom transfer adapter protocol is implemented and tested.
- `local` backend roundtrip path is implemented for deterministic integration testing.
- `sdk` backend spawns `proton-drive-cli bridge` as a subprocess with JSON stdin/stdout protocol.
- No intermediate HTTP bridge layer -- the Go adapter communicates directly with proton-drive-cli.
- Mock transfers are fail-closed by default and require explicit opt-in.

## Prerequisites

- Go 1.25+
- Node.js 25+ (for SEA binary build)
- Yarn 4+ (via Corepack) or npm
- git-lfs
- pass-cli (for credential management, or use git-credential)

No .NET SDK required.

## Documentation

📚 **[Unified Documentation](https://sevenofnine-labs.github.io/proton-lfs-cli/)** - Complete documentation site with:

- **[Architecture & Guides](https://sevenofnine-labs.github.io/proton-lfs-cli/guides/)** - Project overview, architecture, testing, security
- **[Go API Reference](https://pkg.go.dev/github.com/SevenOfNine-labs/proton-lfs-cli)** - Go package documentation
- **[TypeScript Bridge API](https://sevenofnine-labs.github.io/proton-drive-cli/)** - proton-drive-cli documentation

## Usage

See **[USAGE.md](USAGE.md)** for the complete user guide — installation, repository configuration, local and SDK backend walkthroughs, troubleshooting, and CLI reference.

## Quick Start

```bash
git submodule update --init --recursive
make setup
make build-all    # Builds Go adapter, tray app, Git LFS, and proton-drive-cli
make test
make test-integration
make install      # Install .app bundle (macOS) or binaries (Linux)
```

Root JS dependency install (Yarn 4 via Corepack):

```bash
corepack enable
corepack prepare yarn@4.1.1 --activate
yarn install
# fallback
npm install
```

Make-based install:

```bash
make setup
# fallback if you prefer npm
make setup JS_PM=npm
```

Build proton-drive-cli bridge:

```bash
make build-drive-cli
```

SDK-backed integration path:

```bash
pass-cli login
make check-sdk-prereqs
make test-integration-sdk
```

Proton Drive CLI bridge integration path:

```bash
pass-cli login
make test-integration-sdk
```

If your account requires two-factor authentication, complete an interactive
`proton-drive login` before SDK transfers. Browser-fork sessions normally use
the stored UID-scoped key password for Drive unlocks, including accounts that
Proton reports as two-password mode. Configure a separate mailbox/data
credential only if `proton-drive doctor` or bridge `auth-state` explicitly
reports `needs_data_password`:

```bash
git config lfs.customtransfer.proton.args \
  "--backend=sdk --data-credential-provider git-credential"
```

If your Node binary is managed in shell startup (for example `nvm` in `~/.zshrc`), pass it explicitly:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

`make test-integration-sdk` uses `yarn` by default. Override only if needed:

```bash
make test-integration-sdk JS_PM=npm
```

If you use a non-default `pass-cli` binary, export it before running:

```bash
eval "$(make -s pass-env)"
make test-integration-sdk
```

## Credentials

Use browser-fork login for account authorization. Proton Pass or
`git-credential` is used only as the local key-password/data-password provider:

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

`export-pass-env.sh` exports `PROTON_PASS_CLI_BIN` and unsets legacy account
credential references; it does not export Proton usernames or passwords.

## Repository Layout

- `cmd/adapter/`: Go custom transfer adapter (spawns proton-drive-cli directly for SDK backend).
- `cmd/tray/`: System tray app (menu bar status, credential setup, Connect flow).
- `internal/config/`: Shared constants and helpers (adapter + tray).
- `tests/integration/`: black-box Git LFS integration tests.
- `docs/`: project plan, architecture, testing, and operations docs.
- `submodules/`: upstream references (`git-lfs`, `pass-cli`, `proton-drive-cli`).

## Developer Docs

Start at `docs/README.md`.

## Security

This repository is pre-production. See `SECURITY.md`.
