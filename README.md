# Proton LFS CLI

End-to-end encrypted Git LFS backend for Proton Drive. Pre-alpha: see the
[Usage Guide](USAGE.md) for supported platforms and known limitations.
Release notes and upgrade steps are on
[GitHub Releases](https://github.com/SevenOfNine-labs/proton-lfs-cli/releases).

[![Documentation](https://img.shields.io/badge/docs-unified-blue)](https://sevenofnine-labs.github.io/proton-lfs-cli/) [![Go Reference](https://pkg.go.dev/badge/github.com/SevenOfNine-labs/proton-lfs-cli.svg)](https://pkg.go.dev/github.com/SevenOfNine-labs/proton-lfs-cli) [![Tests](https://github.com/SevenOfNine-labs/proton-lfs-cli/actions/workflows/test.yml/badge.svg)](https://github.com/SevenOfNine-labs/proton-lfs-cli/actions/workflows/test.yml)

## Current State

- The Git LFS custom transfer adapter (`git-lfs-proton-adapter`) implements
  the standalone custom transfer protocol and is covered by unit and
  integration tests.
- The `local` backend stores objects on the filesystem for deterministic
  testing; the `sdk` backend runs `proton-drive-cli bridge <command>` as a
  subprocess (JSON on stdin/stdout) and stores objects in Proton Drive.
- Account login is browser-based and happens once in `proton-drive-cli login`;
  transfers reuse the saved session and never carry account credentials.
- A system tray app (`proton-lfs-tray`) shows readiness and transfer status,
  runs the sign-in, keeps the session token fresh and registers the adapter in
  git.
- Mock transfers are fail-closed by default and require explicit opt-in.
- Releases are GitHub release bundles (macOS `.app`, Linux `.tar.gz`, Windows
  `.zip`) plus standalone adapter binaries, with `SHA256SUMS.txt`.

## Prerequisites

- Go 1.25+ (build from source)
- Node.js 22+ to run `proton-drive-cli` from `dist/index.js` and to build the
  vendored Proton SDK; Node.js 26 (`.node-version`) to build the standalone
  `proton-drive-cli` binary (`make build-sea`) and in CI
- Yarn 4 (via Corepack) for proton-drive-cli's dependencies; npm for the
  vendored Proton SDK build
- git-lfs
- pass-cli 2.7.0 when Proton Pass is the credential store; the
  `git-credential` store needs a git credential helper that encrypts secrets
  at rest (`osxkeychain`, `wincred`, `libsecret` or Git Credential Manager)

## Documentation

📚 **[Unified Documentation](https://sevenofnine-labs.github.io/proton-lfs-cli/)** - Complete documentation site with:

- **[Architecture & Guides](https://sevenofnine-labs.github.io/proton-lfs-cli/guides/)** - Project overview, architecture, reference pages, testing, security
- **[Go API Reference](https://pkg.go.dev/github.com/SevenOfNine-labs/proton-lfs-cli)** - Go package documentation
- **[TypeScript Bridge API](https://sevenofnine-labs.github.io/proton-drive-cli/)** - proton-drive-cli documentation, bridge protocol and JSON schemas

## Usage

See **[USAGE.md](USAGE.md)** for the complete user guide: installation,
repository configuration, local and SDK backend walkthroughs, the tray app,
troubleshooting and the CLI reference. The exhaustive tables live in
`docs/reference/`.

## Quick Start

```bash
git submodule update --init --recursive
make setup
make build-all    # Go adapter, tray app, Git LFS, vendored Proton SDK and proton-drive-cli
make test
make test-integration
make install      # .app bundle (macOS) or binaries in ~/.local/bin (Linux)
```

JavaScript dependencies belong to proton-drive-cli (`package.json` and
`yarn.lock` in `submodules/proton-drive-cli/`); Yarn 4 comes from Corepack:

```bash
corepack enable
make setup        # creates .env, runs go mod download and yarn install in the submodule
```

Build proton-drive-cli (the vendored Proton SDK in
`submodules/proton-drive-cli/submodules/sdk/client/js` is built once by
`make build-sdk`; it needs Node.js 22+ and npm, because upstream ships no npm
lockfile):

```bash
make build-sdk
make build-drive-cli
```

SDK-backed integration path (one real Proton Drive round trip per test):

```bash
pass-cli login
proton-drive-cli login --credential-store pass-cli
make check-sdk-prereqs
make test-integration-sdk
```

Two-factor authentication and CAPTCHA are completed in the browser during
`proton-drive-cli login`. The browser fork delivers the key password for
every account type, two-password accounts included; login stores it with
the session tokens in the credential store, and transfers unlock Drive with
it. `proton-drive-cli doctor` checks the setup offline.

If your Node binary is managed in shell startup (for example `nvm` in
`~/.zshrc`), pass it explicitly:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

If you use a non-default `pass-cli` binary, export it before running:

```bash
eval "$(make -s pass-env)"
make test-integration-sdk
```

## Credentials

Account authorization uses browser sign-in. The credential store, Proton
Pass or `git-credential`, holds the session tokens and the browser-derived key
password as one entry keyed by the session UID;
`~/.proton-drive-cli/session.json` holds no secret:

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

`export-pass-env.sh` checks that pass-cli is installed and logged in
(`pass-cli info`) and prints only `export PROTON_PASS_CLI_BIN=<binary>`;
`--pass-cli <bin>` names another binary and `--skip-check` skips the check.

## Repository Layout

- `cmd/adapter/`: Go custom transfer adapter (`main.go` protocol loop,
  `backend.go` local and Proton Drive backends, `bridge.go` proton-drive-cli
  subprocess client).
- `cmd/tray/`: System tray app and `proton-lfs-tray` command-line helpers
  (status, Connect flow, registration, scope diagnostics).
- `internal/config/`: Shared constants, status file and preferences (adapter +
  tray).
- `internal/preflight/`: Parser of `proton-drive-cli doctor --json` used by the
  live canary gates.
- `scripts/`: Build, packaging, install and canary scripts.
- `tests/integration/`: black-box Git LFS integration tests.
- `docs/`: architecture, operations, reference, testing and security docs
  (start at `docs/README.md`).
- `submodules/`: upstream references (`git-lfs` v3.8.1, `pass-cli` 2.7.0,
  `proton-drive-cli`).

## Developer Docs

Start at `docs/README.md`. To contribute, read [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

This repository is pre-production. See `SECURITY.md`.

## License

[MIT](https://github.com/SevenOfNine-labs/proton-lfs-cli/blob/main/LICENSE)
