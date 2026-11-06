# Deployment Guide

This guide is for development and CI environments. The repository is
pre-production; `docs/project/current-state.md` records what is implemented
and what is open.

## Prerequisites

- Go toolchain available.
- `git-lfs` installed and on `PATH`.
- Node.js per `.node-version` (26, the active LTS line) for building the
  `proton-drive-cli` SEA binary and for CI; Node.js 22+ is the runtime floor
  of `proton-drive-cli/dist/index.js` (`engines.node`).
- `pass-cli` 2.7.0 when Proton Pass stores the browser-fork key password;
  the `git-credential` provider needs no extra tool.

No .NET SDK and no internal Proton access are required for any path:

| Backend | Needs a Proton account | Who can run it |
| --- | --- | --- |
| `local` (`PROTON_LFS_BACKEND=local`) | No | Anyone; the default contributor path |
| `sdk` (`PROTON_LFS_BACKEND=sdk`) | Yes, your own | Anyone with a built or bundled `proton-drive-cli` (Node.js 22+ for `dist/index.js`) |

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

`make check-submodules` verifies the root-owned submodules and the
`proton-drive-cli` SDK gitlink without recursing into the Proton SDK (the
pinned js/v0.23.0 records no nested gitlinks; any future ones are listed,
not verified).

Build proton-drive-cli (the vendored Proton SDK is built once by
`make build-sdk`; `make build-all` runs both):

```bash
make build-sdk
make build-drive-cli
```

SDK-backed path:

```bash
pass-cli login
make test-integration-sdk
```

Sign in once with `proton-drive-cli login --key-password-provider <provider>`
before SDK transfers; two-factor authentication and CAPTCHA are completed in
the browser. Browser-fork sessions use
the stored UID-scoped key password for Drive unlocks. Configure a separate
mailbox/data password credential provider only if offline doctor reports
`needs_data_password`, and never use secret environment variables:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --data-credential-provider git-credential"
```

If `node` is not visible to non-interactive shells, pass an explicit binary path:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

Preflight only:

```bash
make check-sdk-prereqs
```

## Installing a Release Bundle

Bundles come from GitHub Releases (`docs/operations/releasing.md`); verify
the download against the release's `SHA256SUMS.txt` first:

```bash
sha256sum -c --ignore-missing SHA256SUMS.txt   # shasum -a 256 -c on macOS
```

### macOS: Gatekeeper and quarantine

`ProtonLFS.app` is ad-hoc signed (`codesign --sign -`), not notarized, so
macOS quarantines the downloaded zip and Gatekeeper refuses to open the app
("cannot be opened because the developer cannot be verified"). After
unzipping, move the app into place and clear the quarantine attribute once:

```bash
unzip proton-lfs-cli-darwin-arm64.zip
mv proton-lfs-cli-darwin-arm64/ProtonLFS.app /Applications/
xattr -dr com.apple.quarantine /Applications/ProtonLFS.app
```

`make install` does the same for a locally built bundle.

### Linux and Windows

Extract `proton-lfs-cli-linux-amd64.tar.gz` or
`proton-lfs-cli-windows-amd64.zip` and put `proton-lfs-tray`,
`git-lfs-proton-adapter` and `proton-drive-cli` on `PATH` (`make install`
copies them to `~/.local/bin` on Linux).

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

- CI holds no Proton credentials: every CI suite is mocked (`scripts/mock-pass-cli.sh`, `tests/testdata/mock-proton-drive-cli.js`), and the real canaries never run there.
- `make test`, the integration suites and the mocked E2E pipeline run on every PR (`.github/workflows/test.yml`).
