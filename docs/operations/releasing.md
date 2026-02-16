# Releasing

This project has two independently versioned artifacts:

| Artifact | Tag pattern | Publish target | Workflow |
| --- | --- | --- | --- |
| Go adapter binary | `v*` (e.g. `v0.2.0`) | GitHub Releases | `.github/workflows/build.yml` |
| Proton Drive CLI npm package | `drive-cli-v*` (e.g. `drive-cli-v0.1.0`) | npm (`@sevenofnine-ai/proton-drive-cli`) | `.github/workflows/npm-publish.yml` |

## Go Adapter

The build workflow cross-compiles for five targets (linux/darwin amd64+arm64, windows amd64), uploads them as artifacts, and — on `v*` tags — creates a GitHub Release with all binaries attached.

### Steps

```bash
# 1. Ensure tests pass
make test && make test-integration

# 2. Tag the release
git tag v0.2.0
git push origin v0.2.0
```

The `build.yml` workflow runs automatically. When the tag matches `v*`, the `release` job creates a GitHub Release with the binaries.

### Verifying

Check the Actions tab or:

```bash
gh release view v0.2.0
```

Users install via the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-ai/proton-lfs-cli/main/scripts/install-adapter.sh | bash
```

Or pin a version:

```bash
VERSION=v0.2.0 curl -fsSL .../scripts/install-adapter.sh | bash
```

## Proton Drive CLI (npm)

Published as `@sevenofnine-ai/proton-drive-cli`. This package provides:

- CLI tool (`proton-drive`) for standalone Proton Drive operations
- Bridge command (`proton-drive-cli bridge <command>`) used by the Go adapter via direct subprocess invocation

### Prerequisites (one-time)

Trusted Publishing must be configured on npmjs.com:

1. Go to npmjs.com → `@sevenofnine-ai/proton-drive-cli` → **Settings** → **Publishing access**
2. Add trusted publisher:
   - **Repository owner**: `SevenOfNine-ai`
   - **Repository name**: `proton-lfs-cli`
   - **Workflow filename**: `npm-publish.yml`
   - **Environment name**: *(blank)*

### npm Publish Steps

```bash
# 1. Ensure tests pass
cd submodules/proton-drive-cli
npx jest

# 2. Bump version in submodules/proton-drive-cli/package.json
npm version patch   # or minor / major / 0.1.1

# 3. Commit the version bump
cd ../..
git add submodules/proton-drive-cli/package.json
git commit -m "drive-cli: bump to v0.1.1"

# 4. Tag and push
git tag drive-cli-v0.1.1
git push origin main --tags
```

The `npm-publish.yml` workflow publishes automatically when the `drive-cli-v*` tag is pushed. The workflow checks out submodules, installs deps, builds TypeScript, runs tests, then publishes.

### Verifying npm Publish

```bash
npm view @sevenofnine-ai/proton-drive-cli version
```

Users install via:

```bash
# As a CLI tool
npm install -g @sevenofnine-ai/proton-drive-cli

# As a library (for bridge imports)
npm install @sevenofnine-ai/proton-drive-cli
```

## Version Numbering

Both artifacts follow semver independently. A Go adapter release does not require a drive-cli release and vice versa. When both artifacts change in the same PR, create both tags:

```bash
git tag v0.3.0
git tag drive-cli-v0.1.1
git push origin main --tags
```

## Manual npm Publish (fallback)

If Trusted Publishing fails or for the initial publish of a new package:

```bash
npm login

# Drive CLI
cd submodules/proton-drive-cli
npm run build
npm publish --access public
```

`--access public` is required on the first publish of a scoped package.
