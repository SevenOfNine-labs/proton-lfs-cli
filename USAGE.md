# Usage Guide

Proton Git LFS Backend stores Git LFS objects on Proton Drive with end-to-end encryption, using a custom Git LFS transfer adapter.

> **Status:** Pre-alpha. The local backend is stable for testing. The SDK (Proton Drive) backend works but depends on proton-drive-cli, which is under active development.

## Prerequisites

| Requirement | Local backend | SDK backend |
| --- | --- | --- |
| Git + git-lfs | Required | Required |
| Go 1.25+ | Build from source only | Build from source only |
| Node.js 18+ | — | Required |
| Yarn 4+ (via Corepack) | — | Required |
| pass-cli | — | Required |

Pre-built adapter binaries are available from GitHub Releases and do not require Go.

## Installation

### Go Adapter (one-line install)

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-ai/proton-git-lfs/main/scripts/install-adapter.sh | bash
```

Override the install directory or version:

```bash
INSTALL_DIR=~/.local/bin VERSION=v0.1.0 curl -fsSL \
  https://raw.githubusercontent.com/SevenOfNine-ai/proton-git-lfs/main/scripts/install-adapter.sh | bash
```

### Method A: Build from source (recommended)

```bash
git clone --recurse-submodules https://github.com/<owner>/proton-git-lfs.git
cd proton-git-lfs

make setup        # Install Go + JS dependencies, create .env
make build-all    # Build adapter, Git LFS submodule, and proton-drive-cli
```

The adapter binary is written to `bin/git-lfs-proton-adapter`.

Optionally, place it on your PATH:

```bash
ln -s "$(pwd)/bin/git-lfs-proton-adapter" ~/.local/bin/git-lfs-proton-adapter
```

### Method B: GitHub Release binary

1. Download the binary for your platform from the GitHub Releases page. Available targets:
   - `git-lfs-proton-adapter-linux-amd64`
   - `git-lfs-proton-adapter-linux-arm64`
   - `git-lfs-proton-adapter-darwin-amd64`
   - `git-lfs-proton-adapter-darwin-arm64`
   - `git-lfs-proton-adapter-windows-amd64.exe`

2. Make it executable and move it onto your PATH:

   ```bash
   chmod +x git-lfs-proton-adapter-darwin-arm64
   mv git-lfs-proton-adapter-darwin-arm64 ~/.local/bin/git-lfs-proton-adapter
   ```

3. Verify:

   ```bash
   git-lfs-proton-adapter --version
   ```

> **Note:** The release binary is only the Go adapter. If you plan to use the SDK backend (Proton Drive), clone the repository and build proton-drive-cli from source (`make build-drive-cli`). The local backend works with just the binary.

## Configuring a Git Repository

### 1. Initialize Git LFS

```bash
cd your-repo
git lfs install
```

### 2. Track large file patterns

```bash
git lfs track "*.psd" "*.bin" "*.zip"
git add .gitattributes
```

### 3. Register the custom transfer adapter

```bash
# Point Git LFS to the adapter binary
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter

# Configure adapter arguments (choose one backend — see sections below)
git config lfs.customtransfer.proton.args "--backend=local --local-store-dir=/path/to/store"

# Tell Git LFS to use the proton adapter for all transfers
git config lfs.standalonetransferagent proton
```

Replace `/path/to/git-lfs-proton-adapter` with the actual path (e.g., `~/.local/bin/git-lfs-proton-adapter` or the absolute path to `bin/git-lfs-proton-adapter` in the cloned repo).

## Local Backend (testing / offline)

The local backend stores LFS objects on the local filesystem. No network access, no credentials, no bridge service. Use it to verify the adapter works before configuring Proton Drive.

### Quick walkthrough

```bash
# 1. Create a bare remote (simulates a Git server)
git init --bare /tmp/lfs-remote.git

# 2. Create a working repo
mkdir /tmp/lfs-test && cd /tmp/lfs-test
git init
git lfs install

# 3. Create a local object store directory
mkdir -p /tmp/lfs-store

# 4. Configure the adapter
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend=local --local-store-dir=/tmp/lfs-store --debug"
git config lfs.standalonetransferagent proton

# 5. Add a remote and track a file type
git remote add origin /tmp/lfs-remote.git
git lfs track "*.bin"
git add .gitattributes

# 6. Create a test file, commit, and push
dd if=/dev/urandom of=test.bin bs=1024 count=4 2>/dev/null
git add test.bin
git commit -m "Add test binary"
git push -u origin main

# 7. Clone into a new directory and verify the roundtrip
cd /tmp
git clone /tmp/lfs-remote.git lfs-clone
cd lfs-clone
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend=local --local-store-dir=/tmp/lfs-store --debug"
git config lfs.standalonetransferagent proton
git lfs pull

# 8. Verify the file matches
diff /tmp/lfs-test/test.bin /tmp/lfs-clone/test.bin && echo "Roundtrip OK"
```

## SDK Backend (Proton Drive)

The SDK backend uploads and downloads LFS objects through Proton Drive with end-to-end encryption. The Go adapter spawns `proton-drive-cli bridge` as a subprocess, communicating via JSON over stdin/stdout.

### 1. Build proton-drive-cli (if not done already)

```bash
cd /path/to/proton-git-lfs
make build-all    # or: make build-adapter && make build-drive-cli
```

### 2. Store credentials in Proton Pass

The adapter resolves credentials through `pass-cli` or `git-credential`. Direct username/password environment variables are not supported.

```bash
pass-cli login
```

Credentials should be stored at the default references:

- `pass://Personal/Proton Git LFS/username`
- `pass://Personal/Proton Git LFS/password`

See [Credential references](#credential-references) below if you use a different vault or item name.

### 3. Export credential references

```bash
eval "$(./scripts/export-pass-env.sh)"
```

Or use the Makefile shorthand:

```bash
eval "$(make -s pass-env)"
```

### 4. Configure the adapter

```bash
cd your-repo
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend=sdk --drive-cli-bin=/path/to/proton-drive-cli"
git config lfs.standalonetransferagent proton
```

### 5. Use Git normally

```bash
git add large-file.psd
git commit -m "Add design file"
git push
```

LFS objects are encrypted and uploaded to Proton Drive automatically.

### 2FA and data password

If your Proton account uses two-factor authentication or a separate data password, set these environment variables before running transfers:

```bash
export PROTON_DATA_PASSWORD='...'
export PROTON_SECOND_FACTOR_CODE='...'
```

## Global vs Per-Repo Configuration

The examples above use per-repo configuration (stored in `.git/config`). To apply settings to all repositories, use `--global`:

```bash
git config --global lfs.customtransfer.proton.path ~/.local/bin/git-lfs-proton-adapter
git config --global lfs.customtransfer.proton.args "--backend=local --local-store-dir=$HOME/.lfs-store"
git config --global lfs.standalonetransferagent proton
```

Per-repo settings override global settings, so you can set a global default and override specific repositories as needed.

## Troubleshooting

### Enable debug logging

Add `--debug` to the adapter arguments:

```bash
git config lfs.customtransfer.proton.args "--backend=local --local-store-dir=/tmp/lfs-store --debug"
```

Debug output is written to stderr, which Git LFS displays during transfers.

### Common issues

| Symptom | Cause | Fix |
| --- | --- | --- |
| `transfer "proton": not found` | Adapter binary not on PATH or `lfs.customtransfer.proton.path` is wrong | Verify the path: `git config lfs.customtransfer.proton.path` |
| `failed to resolve sdk credentials` | pass-cli not logged in or references are wrong | Run `pass-cli login` and check `PROTON_PASS_*` env vars |
| `proton-drive-cli` returns auth error | Session expired or credentials invalid | Re-run `pass-cli login` |
| CAPTCHA required | New Proton accounts may trigger CAPTCHA | Log in via the Proton web app first to clear the CAPTCHA |
| `node not found` in Make targets | Node.js is managed by nvm/fnm and not visible to Make's shell | Pass it explicitly: `make test-integration-sdk NODE="$(command -v node)"` |

### Credential references

The default credential references are:

```
PROTON_PASS_REF_ROOT=pass://Personal/Proton Git LFS
PROTON_PASS_USERNAME_REF=pass://Personal/Proton Git LFS/username
PROTON_PASS_PASSWORD_REF=pass://Personal/Proton Git LFS/password
```

Override them with environment variables or adapter flags if your Proton Pass vault uses different names.

## Adapter CLI Reference

```
git-lfs-proton-adapter [flags]
```

The adapter reads JSON messages from stdin and writes JSON responses to stdout, following the [Git LFS custom transfer protocol](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md).

### Flags

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--backend` | `PROTON_LFS_BACKEND` | `local` | Transfer backend: `local` or `sdk` |
| `--drive-cli-bin` | `PROTON_DRIVE_CLI_BIN` | (auto-detected) | Path to the proton-drive-cli binary (sdk backend only) |
| `--local-store-dir` | `PROTON_LFS_LOCAL_STORE_DIR` | (none) | Directory for local object storage (local backend only) |
| `--allow-mock-transfers` | `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enable mock transfer simulation (testing only) |
| `--proton-pass-cli` | `PROTON_PASS_CLI_BIN` | `pass-cli` | Path to the pass-cli binary |
| `--proton-pass-username-ref` | `PROTON_PASS_USERNAME_REF` | `pass://Personal/Proton Git LFS/username` | pass-cli reference for Proton username |
| `--proton-pass-password-ref` | `PROTON_PASS_PASSWORD_REF` | `pass://Personal/Proton Git LFS/password` | pass-cli reference for Proton password |
| `--debug` | — | `false` | Enable debug logging to stderr |
| `--version` | — | — | Print version and exit |

Environment variables are read as defaults; flags override them.
