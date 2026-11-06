# Adapter Configuration Reference

Source of truth in code: `internal/config/config.go` (names and defaults),
`cmd/adapter/main.go` (flags), `cmd/adapter/bridge.go` (what reaches
proton-drive-cli). The flag-by-flag table is in
[docs/reference/adapter.md](../reference/adapter.md); this page explains how
the settings fit together.

## Environment Variables

Read by `git-lfs-proton-adapter` (each of the first five is the default of
the flag named in the reference page):

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_LFS_BACKEND` | `local` | Adapter backend (`local`, `sdk`) |
| `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enables mock transfer mode (protocol tests only) |
| `PROTON_LFS_LOCAL_STORE_DIR` | empty | Local backend object root |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | proton-drive-cli entry point: a `.js` file run with node, or the standalone executable |
| `PROTON_LFS_BRIDGE_TIMEOUT` | `5m` | Base timeout per bridge command (Go duration); uploads and downloads add 1 s per 256 KiB |
| `NODE_BIN` | `node` on `PATH` | Node.js binary for a `.js` entry point |
| `LFS_STORAGE_BASE` | `LFS` | Remote base folder (`/<base>/ab/cd/<oid>`), sent as the bridge `storageBase` field |
| `PROTON_APP_VERSION` | empty | Optional Proton `x-pm-appversion` override, sent as the bridge `appVersion` field |
| `PROTON_LFS_STATUS_FILE` | `~/.proton-lfs/status.json` | Status file location (also read by the tray) |

Read by proton-drive-cli. The adapter forwards the variables of the first
three rows to the bridge subprocess (they are on its environment
allowlist); the key-password variables are read only by
`proton-drive-cli login` and are not forwarded:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@<cli version>` | proton-drive-cli's own app-version default |
| `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID` | unset | Redacted `[AUTH_TRACE]` lines on stderr and their correlation id; the tray sets both during Connect |
| `PROTON_KEY_PASSWORD_PROVIDER` | `pass-cli` | Provider that `login` stores the browser-fork key password in |
| `PROTON_KEY_PASSWORD_HOST` | `proton-drive-key.proton-lfs-cli.local` | Host/key of that entry |

The Go adapter does **not** resolve Proton credentials and never sends
account or credential fields to proton-drive-cli. Account login is
browser-fork-only and happens outside Git LFS transfers with
`proton-drive-cli login --key-password-provider <provider>`. Transfer bridge
requests carry only `storageBase`, `appVersion` (when set) and the command
fields (`oid`, `path`, `outputPath`, `oids`).

## Key-Password Provider

`proton-drive-cli login` receives the key password from the browser session
fork, for every account type, and stores it, keyed by the session UID, under
`proton-drive-key.proton-lfs-cli.local` in the chosen provider. The CLI
creates and verifies that entry itself; it is never created by hand. The
provider is chosen by `--key-password-provider`, then
`PROTON_KEY_PASSWORD_PROVIDER`, then the default `pass-cli`, and recorded in
the session; every later command (bridge, doctor, logout) uses the recorded
provider and host. The tray stores the choice as `keyPasswordProvider` in
`~/.proton-lfs/config.json` ([Config File](../reference/config-file.md)) and
passes it to `login`.

### pass-cli (default)

```bash
pass-cli login
proton-drive-cli login --key-password-provider pass-cli
```

The entry appears in Proton Pass as a login item titled
`Proton proton-drive-key.proton-lfs-cli.local` with URL
`https://proton-drive-key.proton-lfs-cli.local`. When the lookup fails
(`needs_key_password`), run `pass-cli login` and then `proton-drive-cli login`
again with the same provider; the tray's Connect does not prompt for account
credentials. `PROTON_PASS_CLI_BIN` names a non-default pass-cli binary; the
tray sets it to the binary it finds on `PATH`, `~/.local/bin`,
`/opt/homebrew/bin` or `/usr/local/bin`.

### git-credential

```bash
proton-drive-cli login --key-password-provider git-credential
```

proton-drive-cli stores the key password with `git credential approve` and
reads it back with `git credential fill` for host
`proton-drive-key.proton-lfs-cli.local`; the backing store is whatever git
credential helper is configured (macOS Keychain, Windows Credential Manager,
Linux Secret Service).

### Lifecycle of the entry

- `proton-drive-cli logout` removes it, revokes the session and deletes the
  session file and crypto cache.
- A login that yields a new session UID removes the entry of the previous UID.
- A refresh token the API rejects with a Proton error body and HTTP 4xx
  other than 408 and 429, while no other process has rotated it, signs the
  session out: `session.json`, the
  crypto cache and the entry are deleted, and `auth-state` reports
  `needs_login`.
- A stored key password that does not decrypt the account keys makes
  transfers fail with `key_password_required`; `proton-drive-cli login`
  stores a new one.

## Offline Doctor

Check the configuration offline before a transfer or a live canary:

```bash
proton-drive-cli doctor
proton-drive-cli doctor --key-password-provider git-credential --json
```

Without options the doctor verifies the provider and host recorded in the
session; `--key-password-provider` and `--key-password-host` verify another
entry. It checks the bridge entry point (`drive-cli-bin`), the UID-scoped
key-password entry (`key-password-credential`) and the session file's
permissions and shape (`session-file`) without contacting Proton, and
reports `ok`, `summary`, `authState`, `canAttemptTransfer`,
`canAttemptLiveCanary` and `checks`
(`submodules/proton-drive-cli/docs/reference/doctor.md`). A `ready` result is
a local readiness statement, not a remote session validation: a session
Proton revoked server-side fails at the first SDK call with an auth error
and is never retried with a login.

## Session Refresh

The tray refresh heartbeat is token maintenance only. It calls
`proton-drive-cli session refresh --json` from 10 minutes (at most half the
token lifetime) before `tokenExpiresAt` on; a session without
`tokenExpiresAt` is refreshed 12 hours after `session.json` was last
written. The result preserves redacted HTTP and Proton API details without
printing token values. Recoverable failures and timeouts are retried with
binary exponential backoff from 1 to 30 minutes. Non-recoverable failures are remembered for the same session UID
and the tray does not start login automatically. A rejected refresh token
signs the session out in proton-drive-cli, after which the tray shows
`Not connected`; a successful browser-fork Connect creates a new session and
clears the tray's refresh state.

## Helper Script

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

The script checks that the pass-cli binary exists and is logged in
(`pass-cli info`) and prints one line,
`export PROTON_PASS_CLI_BIN=<binary>`. Options: `--pass-cli <bin>` (default
`$PROTON_PASS_CLI_BIN`, else `pass-cli`) and `--skip-check` (no binary or
login check). `make pass-env` runs it.

## proton-drive-cli App Identity

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_APP_VERSION` | empty | Optional Proton `x-pm-appversion` override forwarded by the Go adapter as the bridge `appVersion` field; must follow the grammar below or proton-drive-cli rejects the request before any network call |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@<cli version>` | proton-drive-cli's default Proton app-version header (version part follows its `package.json`) |

Proton's SDK README requires third-party clients to identify as
`external-drive-{name}@{major.minor.patch}`, optionally followed by a
`-stable`, `-beta` or `-alpha` channel and a `+build` suffix, where `{name}`
uses lowercase letters and underscores. The official `cli-drive` identity is
reserved for Proton's own CLI and is rejected by proton-drive-cli. The Go
default `DefaultDriveCLIAppVersion` must equal proton-drive-cli's default;
`internal/config/app_version_test.go` checks it against the submodule's
`package.json` version.

## Redacted Scope Diagnostics

Use the helper diagnostics command when local readiness is green but Proton
rejects the saved session during the live metadata gate:

```bash
proton-lfs-tray scope-diagnostics
```

This local mode prints JSON evidence only: the key-password provider
selection, session-file presence and mode, a UID fingerprint, token presence
flags, token expiry, local scope names, app-version inputs, and the offline
`bridge auth-state` result. It does not contact Proton.

To perform the one read-only server probe, use the same explicit live canary
acknowledgement:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
  proton-lfs-tray scope-diagnostics --live
```

Live mode adds exactly one `bridge list` request for `/` through
`https://drive-api.proton.me` and reports structured Proton errors such as
`INSUFFICIENT_SCOPE` / API 9101 (exit status 3). It does not run login, init,
upload, download, delete, refresh, or retry loops, and it omits raw token,
password, UID, and session values. If local `authState.state` is not `ready`,
live mode stops before the read-only server probe even when the
acknowledgement is present (exit status 2). Exit statuses and report fields:
[Tray Reference](../reference/tray.md#scope-diagnostics).
