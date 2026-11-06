# Releasing

One artifact family, one tag pattern, one workflow:

| Tag | Workflow | Publishes |
| --- | --- | --- |
| `v*` (e.g. `v0.2.0`) | `.github/workflows/release-bundle.yml` | GitHub Release with platform bundles, standalone adapter binaries, `SHA256SUMS.txt` and a git-cliff changelog |

`proton-drive-cli` is not published to npm. It ships inside the bundles as a
Node.js single executable (SEA) and is otherwise consumed from git (the
submodule pin). The former `drive-cli-v*` tags and `npm-publish.yml` are gone.

## What a release contains

`release-bundle.yml` runs on every `v*` tag:

1. **adapters** — calls `build.yml` (`workflow_call`) to cross-compile the
   standalone adapter for linux/darwin amd64+arm64 and windows amd64:
   `git-lfs-proton-adapter-<os>-<arch>[.exe]`.
2. **build-all** — on native runners (macos-14, ubuntu, windows) builds the
   adapter, the tray (cgo only on macOS) and the `proton-drive-cli` SEA with
   the Node.js version in `.node-version`.
3. **package** — `scripts/package-bundle.sh` assembles
   `proton-lfs-cli-darwin-arm64.zip` (ad-hoc signed `ProtonLFS.app`, zipped
   with `ditto` on macOS), `proton-lfs-cli-linux-amd64.tar.gz` and
   `proton-lfs-cli-windows-amd64.zip`. The Linux and Windows archives are
   reproducible (`SOURCE_DATE_EPOCH` = commit date, sorted entries, numeric
   owner 0:0); the macOS zip is produced by `ditto` from the signed bundle,
   whose code signature makes its bytes differ between builds.
4. **release** — downloads every artifact, writes one `SHA256SUMS.txt`
   covering all of them, generates the changelog with git-cliff
   (`cliff.toml`, verified download, `GITHUB_TOKEN` for PR links) and creates
   the GitHub Release.

`build.yml` on its own (pushes to `main`, pull requests) only uploads the
adapter binaries as workflow artifacts; it never creates a release.

Every binary is stamped from one source: the tag on a release
(`VERSION=<tag>`), `git describe --tags --always --dirty` otherwise.
`scripts/build-go.sh` passes `-X main.Version`, `-X main.GitCommit` and
`-X main.BuildTime` (from `SOURCE_DATE_EPOCH`, defaulting to the commit date,
never the wall clock) with `-trimpath` to both the adapter and the tray;
the Makefile and both workflows call that script.

## Steps

Before tagging a release that changes tray/helper behavior, run
`docs/operations/tray-platform-release-checklist.md`.

```bash
# 1. Ensure submodule pins and tests pass
make check-submodules
make test && make test-integration
make live-canary-preflight

# 2. Tag the release (annotated, signed) and push the tag
git tag -s v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

`make check-submodules` verifies the root-owned submodules and the
`proton-drive-cli` SDK gitlink; nested gitlinks inside the Proton SDK (none
at js/v0.23.0) are reported but not descended into.

## Verifying

```bash
gh release view v0.2.0
gh release download v0.2.0 --pattern SHA256SUMS.txt
```

Users install the standalone adapter via the install script, which verifies
the download against `SHA256SUMS.txt` (`VERIFY_CHECKSUM=auto|required|skip`):

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-labs/proton-lfs-cli/main/scripts/install-adapter.sh | bash
```

Or pin a version:

```bash
curl -fsSL .../scripts/install-adapter.sh | VERSION=v0.2.0 bash
```

The bundles are installed by hand; see `docs/operations/deployment.md`
(including the macOS Gatekeeper step for the ad-hoc signed `.app`).

## Version numbering

Releases follow semver on the `v*` tags. The `proton-drive-cli` submodule
keeps its own `package.json` version (it is the `x-pm-appversion` identity
sent to Proton and is asserted by `internal/config` tests); bump it in the
submodule when the bridge changes and advance the submodule pin in the same
root PR. No separate tag or publish step is needed for it.

## Dry run

`release-bundle.yml` can be started with `workflow_dispatch` on any branch:
it builds and packages everything and uploads the artifacts, but the release
job only runs for `v*` tags.
