# Usage Guide

Proton LFS stores Git LFS objects on Proton Drive with end-to-end encryption
through a custom Git LFS transfer adapter.

> **Status:** Pre-alpha. The local backend is stable for testing. The SDK
> (Proton Drive) backend works and depends on `proton-drive-cli`, which is
> under active development.

## Prerequisites

| Requirement | Local backend | SDK backend | Tray app |
| --- | --- | --- | --- |
| Git + git-lfs | Required | Required | Required |
| Go 1.25+ | Build from source only | Build from source only | Build from source only |
| Node.js 22+ | — | Required to run `proton-drive-cli` from `dist/index.js`; not needed with the bundled standalone binary | Same as SDK backend |
| Node.js 26 (`.node-version`) | — | Build the standalone `proton-drive-cli` binary (`make build-sea`) | Same |
| Yarn 4 (via Corepack) | — | Build from source only | Build from source only |
| pass-cli 2.7.0 | — | When Proton Pass is the credential store | Same |

Pre-built adapter binaries and platform bundles come from GitHub Releases
and need neither Go nor Node.js.

## Installation

### Adapter only (one-line install)

If you only need the transfer adapter (no tray app, no Proton Drive):

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-labs/proton-lfs-cli/main/scripts/install-adapter.sh | bash
```

Options are environment variables: `INSTALL_DIR` (default `/usr/local/bin`),
`VERSION` (a release tag, default latest) and `VERIFY_CHECKSUM`
(`required`, the default, verifies the download against the release's
`SHA256SUMS.txt` and fails when the file, the asset's entry or a matching
hash is missing; `skip` installs without a check).

```bash
curl -fsSL https://raw.githubusercontent.com/SevenOfNine-labs/proton-lfs-cli/main/scripts/install-adapter.sh \
  | INSTALL_DIR=~/.local/bin VERSION=v0.2.0 bash
```

> **Note:** The script installs only the Go adapter binary, which is all the
> local backend needs. For the SDK backend (Proton Drive) or the tray app use
> a release bundle or build the full bundle below.

### Release bundle (adapter + tray + proton-drive-cli)

Each release ships one archive per platform, listed in `SHA256SUMS.txt`:

| Asset | Contents |
| --- | --- |
| `proton-lfs-cli-darwin-arm64.zip` | `ProtonLFS.app` with `Contents/MacOS/proton-lfs-tray`, `Contents/Helpers/git-lfs-proton-adapter`, `Contents/Helpers/proton-drive-cli` |
| `proton-lfs-cli-linux-amd64.tar.gz` | `proton-lfs-tray`, `git-lfs-proton-adapter`, `proton-drive-cli`, `proton-lfs.desktop` |
| `proton-lfs-cli-windows-amd64.zip` | the same three programs as `.exe` |
| `git-lfs-proton-adapter-<os>-<arch>[.exe]` | standalone adapter for linux amd64/arm64, darwin amd64/arm64, windows amd64 |

Verify, then install:

```bash
sha256sum -c --ignore-missing SHA256SUMS.txt      # shasum -a 256 -c on macOS
```

- macOS: unzip, move `ProtonLFS.app` to `/Applications`, then clear the
  quarantine attribute once (`xattr -dr com.apple.quarantine /Applications/ProtonLFS.app`);
  the app is ad-hoc signed, not notarized.
- Linux and Windows: extract and put the three programs on `PATH`.

After installing or updating, write the global Git LFS settings for the
installed programs and sign in whenever `proton-lfs-tray status` does not
report `signed in` or `proton-drive-cli doctor` fails:

```bash
proton-lfs-tray register
proton-drive-cli login        # or Connect to Proton in the tray
```

See [Deployment](docs/operations/deployment.md) for details.

### Full bundle from source

```bash
git clone --recurse-submodules https://github.com/SevenOfNine-labs/proton-lfs-cli.git
cd proton-lfs-cli

make setup            # Go + JS dependencies, .env from .env.example
make install          # build adapter, tray and the proton-drive-cli SEA, then install
```

| Platform | Default location | Override |
| --- | --- | --- |
| macOS | `/Applications/ProtonLFS.app` | `INSTALL_APP=/path/to/App.app make install` |
| Linux | `~/.local/bin/` | `INSTALL_BIN=/usr/local/bin make install` |
| Windows | not supported by `make install`; use the release `.zip` | — |

`make install` also links the tray binary as `~/.local/bin/proton-lfs-cli`
(`INSTALL_BIN`), the name under which its command-line helpers are usually
invoked:

```bash
proton-lfs-cli --version
```

`make uninstall` removes everything `make install` placed.

The macOS bundle layout:

```text
ProtonLFS.app/
  Contents/
    MacOS/proton-lfs-tray            system tray executable
    Helpers/git-lfs-proton-adapter   transfer adapter
    Helpers/proton-drive-cli         Proton Drive client (standalone Node.js build)
    Resources/AppIcon.icns
    Info.plist
```

Launch the app from `/Applications` or Spotlight; the icon appears in the
menu bar. On Linux run `proton-lfs-tray` by hand or enable "Start at System
Login" in its menu.

### WSL2 on Windows

WSLg has no system tray, so WSL2 uses the command-line programs; the
`proton-lfs-tray` binary still serves `register`, `status`, `login` and
`logout`. Install Go 1.25 and Node.js 26, then:

```bash
make setup
make install          # ~/.local/bin: proton-lfs-tray, proton-lfs-cli, git-lfs-proton-adapter, proton-drive-cli
proton-lfs-cli register
```

Keep the session entry in Windows Credential Manager through Git for
Windows' Git Credential Manager, for this host only; proton-drive-cli
passes its settings to the Windows process through `WSLENV`:

```bash
git config --global 'credential.https://proton-drive-cli.invalid.helper' \
  '/mnt/c/Program\ Files/Git/mingw64/bin/git-credential-manager.exe'
proton-drive-cli login --credential-store git-credential
```

`proton-drive-cli login` prints the sign-in URL; open it in the Windows
browser. Without the tray nothing refreshes the token in the background:
each transfer refreshes it when it has expired. pass-cli keeps its own
Linux login in the kernel keyring, which a WSL restart clears.

### Adapter only (build from source)

```bash
make setup
make build-adapter    # bin/git-lfs-proton-adapter
ln -s "$(pwd)/bin/git-lfs-proton-adapter" ~/.local/bin/git-lfs-proton-adapter
```

Building `proton-drive-cli` as well: `make build-drive-cli` (produces
`submodules/proton-drive-cli/dist/index.js`, run with Node.js 22+), or
`make build-sea` for the standalone binary (needs Node.js 26).

## System Tray App

The tray app shows readiness and transfer status, runs the browser sign-in,
keeps the session token fresh, and registers the adapter in git. Everything
below is detailed in the [Tray Reference](docs/reference/tray.md).

### Menu

```text
Proton LFS <version>
─────────────────────────────
Status: Ready
Session: Signed in
Transfers: Ready
Refresh: Due in 23h
─────────────────────────────
Disconnect from Proton          (Connect to Proton... when signed out)
Recheck Status
─────────────────────────────
✓ Use Proton Pass
  Use Git Credential Helper
─────────────────────────────
✓ LFS Backend Enabled           (Enable LFS Backend before registration)
─────────────────────────────
Open Logs
Run Doctor...
✓ Start at System Login
─────────────────────────────
Quit Proton LFS
```

`Status` is `Ready` when a session file written by `proton-drive-cli login`
exists (with `uid`, credential store and host, and `revision`) and no
non-recoverable refresh failure is recorded for it; an expired access token
only adds `token refresh due` to the session line, because transfers refresh
it; otherwise it reads `Setup needed` (signed in but blocked) or
`Not connected`. Readiness comes from `session.json` alone; the session
refresh and logout the tray starts read and write the session entry through
proton-drive-cli. A missing session entry is reported when a transfer starts,
by a failed refresh and by `Run Doctor...`.

### Status indicators

The icon has four images; the adapter's status file has six states:

| Icon | When | Meaning |
| --- | --- | --- |
| Grey | No session file | Not connected |
| Green | Ready, and the status file is missing or reports `ok` | Ready |
| Blue | Signed in and ready, status `transferring` | Transfer in progress (tooltip: Uploading…/Downloading…); the adapter itself never writes this state |
| Red | Readiness blocked (`Setup needed`), or status `error`, `rate_limited`, `auth_required`, `captcha` | The tooltip names which |

Status is read from `~/.proton-lfs/status.json` every 5 seconds; see
[Status File Reference](docs/reference/status-file.md).

### Credential store

`Use Proton Pass` and `Use Git Credential Helper` choose where
`proton-drive-cli login` stores the session tokens and the browser-derived
key password:

- Proton Pass: through `pass-cli` (run `pass-cli login` first).
- Git Credential Helper: git's credential helper that encrypts secrets at
  rest (macOS Keychain, Windows Credential Manager, Linux Secret Service, or
  Git Credential Manager).

The choice is saved as `credentialStore` in `~/.proton-lfs/config.json`
(default `pass-cli`). See [Credential stores](#credential-stores).

### Connect to Proton

`Connect to Proton...` runs
`proton-drive-cli login --credential-store <store> -q`. A browser
window opens on Proton's sign-in page, where two-factor authentication and
CAPTCHA are completed too; after approval the CLI stores the session tokens
and the key password the fork delivers in the chosen credential store and
records the session in `~/.proton-drive-cli/session.json`, which holds no
secret. The tray then shows `Proton LFS Ready`,
or `Signed in; setup needed` when a local blocker remains. If Proton
rate-limited the sign-in, the tray refuses another attempt for one hour.

The tray refreshes the session token from 10 minutes (at most half the
token lifetime) before it expires on; a session without a recorded expiry is
refreshed 12 hours after its last write. A recoverable failure is retried
after 1, 2, 4, ... minutes, at most every 30 minutes. When Proton rejects the
refresh token, proton-drive-cli signs the session out and the menu shows
`Not connected` until you connect again.

### Enable LFS Backend

Writes the three `git config --global` keys described in
[Register the custom transfer adapter](#3-register-the-custom-transfer-adapter)
with `--backend sdk --drive-cli-bin <bundled proton-drive-cli>`.
`proton-lfs-tray register` does the same from the command line.

### Open Logs and Run Doctor

`Open Logs` tails `~/.proton-lfs/tray.log` in a terminal. `Run Doctor...`
runs `proton-drive-cli doctor`, which reads the session entry from the
credential store recorded in the session; the doctor is offline and reports whether a
transfer can be attempted.

### Start at System Login

macOS: creates or removes `~/Library/LaunchAgents/com.proton.lfs-tray.plist`.
Linux: creates or removes `~/.config/autostart/proton-lfs.desktop`.

### Command-line helpers

```text
proton-lfs-tray login             browser sign-in with the configured credential store
proton-lfs-tray logout            clear the session
proton-lfs-tray register          write the global git LFS registration
proton-lfs-tray status            session, LFS, credential store, transfer, readiness, refresh
proton-lfs-tray scope-diagnostics [--live]
                                  redacted auth/scope evidence
proton-lfs-tray config [store]    show or set the credential store
```

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

# Configure adapter arguments (choose one backend; see the sections below)
git config lfs.customtransfer.proton.args "--backend local --local-store-dir /path/to/store"

# Route all transfers through the proton adapter (standalone mode, no LFS server)
git config lfs.standalonetransferagent proton
```

Replace `/path/to/git-lfs-proton-adapter` with the real path, for example
`~/.local/bin/git-lfs-proton-adapter` or the adapter inside the `.app`
bundle.

> **Tip:** With the tray app running, "Enable LFS Backend" writes these keys
> globally for the SDK backend.

## Local Backend (testing / offline)

The local backend stores objects on the filesystem under
`<store-dir>/<oid[0:2]>/<oid[2:4]>/<oid>`. No network, no credentials.

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
git config lfs.customtransfer.proton.args "--backend local --local-store-dir /tmp/lfs-store --debug"
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
git config lfs.customtransfer.proton.args "--backend local --local-store-dir /tmp/lfs-store --debug"
git config lfs.standalonetransferagent proton
git lfs pull

# 8. Verify the file matches
diff /tmp/lfs-test/test.bin /tmp/lfs-clone/test.bin && echo "Roundtrip OK"
```

## SDK Backend (Proton Drive)

The SDK backend uploads and downloads objects through Proton Drive with
end-to-end encryption. For every bridge command the adapter starts
`proton-drive-cli bridge <command>` as a subprocess and exchanges JSON over
stdin/stdout. Objects are stored at `/LFS/<oid[0:2]>/<oid[2:4]>/<oid>`
(`LFS_STORAGE_BASE` changes the base folder).

### 1. Get proton-drive-cli

Use the binary from a release bundle, or build it:

```bash
make build-drive-cli      # submodules/proton-drive-cli/dist/index.js (Node.js 22+)
make build-sea            # bin/proton-drive-cli standalone binary (Node.js 26)
```

### 2. Sign in once

Account login is browser-based and happens before any transfer:

```bash
pass-cli login                                       # only for the pass-cli store
proton-drive-cli login --credential-store pass-cli   # or git-credential
proton-drive-cli doctor                              # offline readiness check
```

`login` stores the session tokens and the browser-derived key password in
the credential store (see [Credential stores](#credential-stores)) and
records the session in `~/.proton-drive-cli/session.json`. The tray's
"Connect to Proton..." runs the same command.

### 3. Configure the adapter

```bash
cd your-repo
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend sdk --drive-cli-bin /path/to/proton-drive-cli"
git config lfs.standalonetransferagent proton
```

`--drive-cli-bin` accepts the standalone binary or `dist/index.js` (run with
`node`, or `NODE_BIN`). The credential store is not an adapter flag:
the adapter never logs in, it reuses the saved session.

### 4. Use git normally

```bash
git add large-file.psd
git commit -m "Add design file"
git push
```

Before each transfer session the adapter runs the `bridge auth-state` check
and refuses to transfer unless it reports `ready` or `session_expired`
(`bridge init` refreshes an expired access token); the error names what to
do (see [Troubleshooting](#troubleshooting)).

### Two-factor authentication and CAPTCHA

Both are completed in the browser during `proton-drive-cli login`. Transfer
commands never prompt; a CAPTCHA demanded during a transfer stops it with
`captcha_required`.

### Two-password accounts

The browser fork delivers the key password for every account type, so an
account with a separate mailbox password needs no extra setup: transfers
unlock Drive with the key password from the credential store.

## Credential stores

`proton-drive-cli login` receives the session tokens and a key password from
the browser sign-in and stores them as one entry, keyed by the session UID,
under the host `proton-drive-cli.invalid` in one of two credential stores.
`~/.proton-drive-cli/session.json` records only the UID, scopes, store, host,
token expiry and a revision; it holds no secret. The CLI creates, verifies
and rewrites the entry itself (every token refresh replaces it); you never
create it by hand. A session file without a credential store or revision is
not a login session: `proton-drive-cli login` replaces it.

### Proton Pass (pass-cli, default)

```bash
pass-cli login                                     # authenticate pass-cli
proton-drive-cli login --credential-store pass-cli
```

The entry appears in Proton Pass as a login item titled
`Proton proton-drive-cli.invalid`; every refresh replaces it with a new item
and deletes the old one. `pass-cli info` shows whether pass-cli itself is
logged in. `PROTON_PASS_CLI_BIN` overrides the pass-cli binary path (default
`pass-cli` on `PATH`); the tray sets it for the bundled environment
automatically.

### Git credential helper (git-credential)

```bash
proton-drive-cli login --credential-store git-credential
```

The entry is stored with `git credential approve` for host
`proton-drive-cli.invalid` and read back with `git credential fill`
(git-credential(1)). proton-drive-cli runs only the first helper of the
system and global git configuration that git applies to the host, from
`credential.helper` or a `credential.<url>.helper` such as
`credential.https://proton-drive-cli.invalid.helper` (gitcredentials(7),
"CREDENTIAL CONTEXTS"), and that encrypts secrets at rest: `osxkeychain`
(macOS Keychain), `wincred` (Windows Credential Manager), `libsecret` (Linux
Secret Service), or `manager` (Git Credential Manager) with an encrypting
store (`keychain`, `wincredman`, `dpapi`, `secretservice` or `gpg`). `store` and `cache` are never used. git
and Git Credential Manager do not prompt for credentials; the operating
system may still ask to unlock a locked keychain or keyring.

A helper for this host only leaves every other remote untouched:

```bash
git config --global 'credential.https://proton-drive-cli.invalid.helper' osxkeychain
```

### Store selection order

`--credential-store`, then `PROTON_CREDENTIAL_STORE`, then `pass-cli`. The
tray passes the store from `~/.proton-lfs/config.json`.
`--credential-host` / `PROTON_CREDENTIAL_HOST` change the host (rarely
needed). `login` records both in the session; every later command (bridge,
doctor, logout, session refresh) uses the recorded values.

### Lifecycle of the session entry

Every token refresh rewrites the entry. `proton-drive-cli logout` (or
`Disconnect from Proton`) removes it; a login that yields a new session UID
removes the previous UID's entry; a refresh token Proton rejects signs the
session out and removes the entry.

## Global vs Per-Repo Configuration

The examples above use per-repository configuration (`.git/config`). For all
repositories use `--global`:

```bash
git config --global lfs.customtransfer.proton.path ~/.local/bin/git-lfs-proton-adapter
git config --global lfs.customtransfer.proton.args "--backend local --local-store-dir $HOME/.lfs-store"
git config --global lfs.standalonetransferagent proton
```

Per-repository settings override global ones.

## Troubleshooting

### Enable debug logging

Add `--debug` to the adapter arguments; output goes to stderr, which git-lfs
shows with `GIT_TRACE=1`:

```bash
git config lfs.customtransfer.proton.args "--backend local --local-store-dir /tmp/lfs-store --debug"
GIT_TRACE=1 git push
```

### Common issues

| Symptom | Cause | Fix |
| --- | --- | --- |
| `transfer "proton": not found` | `lfs.customtransfer.proton.path` is wrong or the adapter is not executable | `git config lfs.customtransfer.proton.path` and check the file |
| `no ready Proton Drive session; run proton-drive-cli login …` (`needs_login`) | No saved session | `proton-drive-cli login --credential-store <store>` or "Connect to Proton..." |
| `Proton Drive session is invalid …` (`session_invalid`) | The session file was not written by `proton-drive-cli login` | Run `proton-drive-cli login` again |
| `session is invalid or expired` (`auth_required`) | Proton rejected the refresh token and proton-drive-cli signed the session out | Run `proton-drive-cli login` again |
| `Proton session credentials are unavailable from the credential store` (`credentials_unavailable`) | The credential store recorded in the session cannot return the session entry (pass-cli logged out, item deleted, keyring locked, no OS-store credential helper) or its key password does not unlock the account keys | Fix the store (for pass-cli run `pass-cli login`); otherwise `proton-drive-cli login` again |
| `Proton rejected this app/session authorization scope` (`insufficient_scope`, Proton 9101) | The app identity or session scope is rejected by Proton | `proton-lfs-tray scope-diagnostics`, then follow `docs/operations/live-canary-runbook.md`; do not retry login |
| `rate limited by proton api` | Proton rate limit | Wait; the tray blocks sign-in for one hour after a rate-limited login |
| `captcha verification required` | Proton requires CAPTCHA | Run `proton-drive-cli login` and complete it in the browser |
| `bridge <command> timed out after 5m0s` | Large object or slow network exceeded the base timeout | Add `--bridge-timeout 20m` (or `PROTON_LFS_BRIDGE_TIMEOUT`) to the adapter arguments |
| `node not found` in Make targets | Node.js is managed by nvm/fnm and not visible to Make's shell | `make test-integration-sdk NODE="$(command -v node)"` |
| Tray icon stays grey | No session file | "Connect to Proton..." |
| "Error: CLI not found" in the tray | `proton-drive-cli` is not next to the tray binary | Reinstall with `make install` or check the `.app` bundle structure |
| `Refresh: Reconnect required` | A non-recoverable refresh failure was recorded for the saved session | "Connect to Proton..." creates a new session |
| Tray switches to `Not connected` after a refresh | Proton rejected the refresh token; proton-drive-cli signed the session out | "Connect to Proton..." |

## Adapter CLI Reference

```text
git-lfs-proton-adapter [flags]
```

The adapter reads JSON messages from stdin and writes JSON responses to
stdout following the
[Git LFS custom transfer protocol](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md).
`git-lfs-proton-adapter --help` prints the full usage text.

### Flags

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--backend` | `PROTON_LFS_BACKEND` | `local` | Transfer backend: `local` or `sdk` |
| `--drive-cli-bin` | `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | proton-drive-cli entry point: a `.js` file run with node, or the standalone executable (sdk backend) |
| `--local-store-dir` | `PROTON_LFS_LOCAL_STORE_DIR` | (none) | Object directory of the local backend |
| `--allow-mock-transfers` | `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Simulate transfers (protocol tests only) |
| `--bridge-timeout` | `PROTON_LFS_BRIDGE_TIMEOUT` | `5m` | Base timeout per bridge command; uploads and downloads add 1 s per 256 KiB |
| `--debug` | — | `false` | Debug logging to stderr |
| `--version` | — | — | Print version and exit |

Environment-only settings: `NODE_BIN` (Node.js binary for a `.js` entry
point), `LFS_STORAGE_BASE` (remote base folder, default `LFS`),
`PROTON_APP_VERSION` (Proton app-version override), `PROTON_LFS_STATUS_FILE`
(status file path). Environment variables are read as defaults; flags
override them. The full reference, including the variables forwarded to
proton-drive-cli, is [docs/reference/adapter.md](docs/reference/adapter.md).
