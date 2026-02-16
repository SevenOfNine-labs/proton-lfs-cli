# Usage Guide

Proton LFS Backend stores Git LFS objects on Proton Drive with end-to-end encryption, using a custom Git LFS transfer adapter.

> **Status:** Pre-alpha. The local backend is stable for testing. The SDK (Proton Drive) backend works but depends on proton-drive-cli, which is under active development.

## Prerequisites

| Requirement | Local backend | SDK backend | System tray app |
| --- | --- | --- | --- |
| Git + git-lfs | Required | Required | Required |
| Go 1.25+ | Build from source only | Build from source only | Build from source only |
| Node.js 25+ | — | Required | Required (for SEA build) |
| Yarn 4+ (via Corepack) | — | Required | Required |
| pass-cli | — | If using Proton Pass | If using Proton Pass |

Pre-built adapter binaries are available from GitHub Releases and do not require Go.

## Installation

### Go Adapter only (one-line install)

If you only need the transfer adapter (no system tray app):

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-ai/proton-lfs-cli/main/scripts/install-adapter.sh | bash
```

Override the install directory or version:

```bash
INSTALL_DIR=~/.local/bin VERSION=v0.1.0 curl -fsSL \
  https://raw.githubusercontent.com/SevenOfNine-ai/proton-lfs-cli/main/scripts/install-adapter.sh | bash
```

> **Note:** The one-line install only installs the Go adapter binary. The local backend works with just this binary. For the SDK backend (Proton Drive) or the system tray app, use the full bundle install below.

### Full bundle (adapter + tray app + proton-drive-cli)

The full bundle includes all three components needed for a complete Proton Drive LFS setup:

- **git-lfs-proton-adapter** — the Git LFS custom transfer adapter
- **proton-lfs-cli-tray** — system tray app for status, credential setup, and login
- **proton-drive-cli** — Proton Drive client (handles auth, encryption, file transfer)

#### Build and install

```bash
git clone --recurse-submodules https://github.com/<owner>/proton-lfs-cli.git
cd proton-lfs-cli

make setup            # Install Go + JS dependencies, create .env
make install          # Build all components and install
```

`make install` builds the adapter, tray app, and proton-drive-cli SEA binary, then installs them:

| Platform | Default location | Override |
| --- | --- | --- |
| macOS | `/Applications/ProtonGitLFS.app` | `INSTALL_APP=/path/to/App.app make install` |
| Linux | `~/.local/bin/` | `INSTALL_BIN=/usr/local/bin make install` |

On both platforms, `make install` also places a `proton-lfs-cli` CLI entry point on your PATH (`~/.local/bin/proton-lfs-cli`):

```bash
proton-lfs-cli --version
```

To uninstall:

```bash
make uninstall
```

#### macOS .app bundle

On macOS, `make install` creates a standard `.app` bundle:

```
ProtonGitLFS.app/
  Contents/
    MacOS/proton-lfs-cli-tray       ← system tray executable
    Helpers/git-lfs-proton-adapter  ← transfer adapter
    Helpers/proton-drive-cli        ← Proton Drive client (SEA binary)
    Info.plist
```

Launch the app from `/Applications` or Spotlight. The tray icon appears in the menu bar.

#### Linux

On Linux, the three binaries are installed to `~/.local/bin/` (or your chosen `INSTALL_BIN`). Run the tray app manually or set it to autostart (the tray app includes an autostart toggle in its menu).

### Adapter only (build from source)

If you don't need the tray app or proton-drive-cli SEA binary:

```bash
make setup
make build-adapter    # Builds bin/git-lfs-proton-adapter only
```

Optionally, place it on your PATH:

```bash
ln -s "$(pwd)/bin/git-lfs-proton-adapter" ~/.local/bin/git-lfs-proton-adapter
```

### GitHub Release binary

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

## System Tray App

The system tray app provides a menu bar interface for managing Proton LFS. It monitors transfer status, handles credential setup, and manages Proton login sessions.

### Menu overview

```
Proton LFS v...
─────────────────────────────
Credential Store              >
  ✓ Git Credential Manager
    Proton Pass
─────────────────────────────
  Connect to Proton…
  Enable LFS Backend
─────────────────────────────
  Start at System Login
─────────────────────────────
Quit
```

After connecting and enabling LFS, the checkmarks update automatically:

```
✓ Connected to Proton
✓ LFS Backend Enabled
✓ Start at System Login
```

### Status indicators

**Tray icon** changes color based on the adapter's state:

| Icon | State | Meaning |
| --- | --- | --- |
| Grey | idle | No recent transfers |
| Green | ok | Last transfer succeeded |
| Red | error | Last transfer failed |
| Blue | transferring | Transfer in progress |

**Menu checkmarks** next to "Connect to Proton" and "Enable LFS Backend" update every 5 seconds based on session file and git config state. Transfer direction (uploading/downloading) is shown in the tray tooltip.

Status is read from `~/.proton-lfs-cli/status.json`, polled every 5 seconds.

### Credential Store

Choose where your Proton credentials are stored:

- **Git Credential Manager** — uses the system's git credential helper (macOS Keychain, Windows Credential Manager, or Linux Secret Service)
- **Proton Pass** — uses Proton Pass CLI (`pass-cli`) for encrypted credential storage

See [Credential providers](#credential-providers) below for setup details.

### Connect to Proton

The "Connect to Proton..." button handles the full authentication flow automatically:

1. **Checks** if credentials are already stored (silent subprocess)
2. **If missing** — opens a Terminal window for interactive credential setup
3. **If present** — logs in to Proton silently in the background
4. **Shows feedback** via native OS notification ("Connected to Proton", "Login failed", etc.)

After a successful connection, the checkmark appears within 5 seconds. The tray app also refreshes the session token every 15 minutes to keep it alive.

### Enable LFS Backend

Registers the adapter and proton-drive-cli with Git's global config. This is equivalent to running the three `git config --global` commands manually (see [Register the custom transfer adapter](#3-register-the-custom-transfer-adapter)).

### Start at System Login

Toggles automatic launch at login:

- **macOS**: creates/removes a LaunchAgent plist
- **Linux**: creates/removes an XDG autostart `.desktop` file

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

> **Tip:** If you installed via `make install` and launched the tray app, click "Enable LFS Backend" to run these commands automatically.

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
cd /path/to/proton-lfs-cli
make build-all    # or: make build-adapter && make build-drive-cli
```

### 2. Set up credentials

The adapter resolves credentials through one of two providers. Direct username/password environment variables are not supported. See [Credential providers](#credential-providers) below for setup instructions.

### 3. Configure the adapter

```bash
cd your-repo
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend=sdk --drive-cli-bin=/path/to/proton-drive-cli"
git config lfs.standalonetransferagent proton
```

To use git-credential instead of pass-cli, add the credential provider flag:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --credential-provider git-credential --drive-cli-bin=/path/to/proton-drive-cli"
```

### 4. Use Git normally

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

## Credential Providers

The adapter supports two credential providers, controlled by `PROTON_CREDENTIAL_PROVIDER` or `--credential-provider`:

### Git Credential Manager (git-credential)

Uses the system's git credential helper to store and retrieve credentials. This is the simplest setup on macOS (Keychain) and Windows (Credential Manager).

**Setup:**

```bash
# Store your Proton credentials in the system credential helper
proton-drive-cli credential store -u your.email@proton.me

# Verify they are stored
proton-drive-cli credential verify
```

Or, if using the tray app: select "Git Credential Manager" in the Credential Store menu, then click "Connect to Proton...".

**How it works:** The adapter sends `{ "credentialProvider": "git-credential" }` to proton-drive-cli, which resolves credentials locally via `git credential fill`. Credentials never leave the local machine.

### Proton Pass (pass-cli)

Uses Proton Pass CLI for encrypted credential storage. This is the default provider.

**Setup:**

```bash
# Log in to Proton Pass (opens browser for OAuth)
pass-cli login
```

Credentials should be stored as a login item with a `proton.me` URL in any vault. `proton-drive-cli` searches all vaults for the first matching entry.

**Setup via CLI:**

```bash
proton-drive credential store --provider pass-cli   # Create entry interactively
proton-drive credential verify --provider pass-cli  # Verify entry exists
```

**How it works:** The adapter sends `{ "credentialProvider": "pass-cli" }` to proton-drive-cli, which resolves credentials by searching Proton Pass vaults internally. The Go adapter never sees raw credentials.

The `PROTON_PASS_CLI_BIN` environment variable can override the pass-cli binary path (default: `pass-cli`).

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
| `proton-drive-cli` returns auth error | Session expired or credentials invalid | Re-run `pass-cli login` or click "Connect to Proton..." in the tray app |
| CAPTCHA required | New Proton accounts may trigger CAPTCHA | Log in via the Proton web app first to clear the CAPTCHA |
| `node not found` in Make targets | Node.js is managed by nvm/fnm and not visible to Make's shell | Pass it explicitly: `make test-integration-sdk NODE="$(command -v node)"` |
| Tray icon stays grey | No status file yet (no transfers have run) | Push or pull an LFS object to generate `~/.proton-lfs-cli/status.json` |
| "Error: CLI not found" in tray | proton-drive-cli not found relative to tray binary | Reinstall with `make install` or verify the `.app` bundle structure |

## Adapter CLI Reference

```
git-lfs-proton-adapter [flags]
```

The adapter reads JSON messages from stdin and writes JSON responses to stdout, following the [Git LFS custom transfer protocol](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md).

### Flags

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--backend` | `PROTON_LFS_BACKEND` | `local` | Transfer backend: `local` or `sdk` |
| `--credential-provider` | `PROTON_CREDENTIAL_PROVIDER` | `pass-cli` | Credential provider: `pass-cli` or `git-credential` |
| `--drive-cli-bin` | `PROTON_DRIVE_CLI_BIN` | (auto-detected) | Path to the proton-drive-cli binary (sdk backend only) |
| `--local-store-dir` | `PROTON_LFS_LOCAL_STORE_DIR` | (none) | Directory for local object storage (local backend only) |
| `--allow-mock-transfers` | `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enable mock transfer simulation (testing only) |
| `--debug` | — | `false` | Enable debug logging to stderr |
| `--version` | — | — | Print version and exit |

Environment variables are read as defaults; flags override them.
