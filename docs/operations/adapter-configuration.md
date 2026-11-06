# Adapter Configuration Reference

Source of truth in code: `internal/config/config.go` (names and defaults),
`cmd/adapter/main.go` (flags), `cmd/adapter/bridge.go` (what reaches
proton-drive-cli). The flag-by-flag table is in
[docs/reference/adapter.md](../reference/adapter.md); this page explains how
the settings fit together.

## Environment Variables

Read by `git-lfs-proton-adapter` (each is the default of the flag named in
the reference page):

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_LFS_BACKEND` | `local` | Adapter backend (`local`, `sdk`) |
| `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enables mock transfer mode (protocol tests only) |
| `PROTON_LFS_LOCAL_STORE_DIR` | empty | Local backend object root |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | proton-drive-cli entry point: a `.js` file run with node, or the standalone executable |
| `NODE_BIN` | `node` on `PATH` | Node.js binary for a `.js` entry point |
| `PROTON_LFS_BRIDGE_TIMEOUT` | `5m` | Base timeout per bridge command (Go duration); uploads and downloads add 1 s per 256 KiB |
| `PROTON_DATA_CREDENTIAL_PROVIDER` | empty | Optional provider of the mailbox/data password (`pass-cli`, `git-credential`) |
| `PROTON_DATA_CREDENTIAL_HOST` | `proton-data.proton-lfs-cli.local` | Credential host/key of that entry |
| `LFS_STORAGE_BASE` | `LFS` | Remote base folder (`/<base>/ab/cd/<oid>`) |
| `PROTON_APP_VERSION` | empty | Optional Proton `x-pm-appversion` override, forwarded as the bridge `appVersion` field |
| `PROTON_LFS_STATUS_FILE` | `~/.proton-lfs/status.json` | Status file location |

Forwarded unchanged to proton-drive-cli when set (they are on the
subprocess environment allowlist):

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_KEY_PASSWORD_PROVIDER` | `pass-cli` | Provider of the browser-fork key password, used by `proton-drive-cli login` and `doctor` |
| `PROTON_KEY_PASSWORD_HOST` | `proton-drive-key.proton-lfs-cli.local` | Host/key of that entry |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@<cli version>` | proton-drive-cli's own app-version default |
| `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID` | unset | Redacted `[AUTH_TRACE]` lines on stderr and their correlation id; the tray sets both during Connect |

The Go adapter does **not** resolve Proton account credentials and never
sends account username/password selectors to proton-drive-cli. Account login
is browser-fork-only and happens outside Git LFS transfers with
`proton-drive-cli login --key-password-provider <provider>`. Transfer bridge
requests carry only `storageBase`, optional `appVersion`, and, for
two-password accounts, `{ "dataCredentialProvider": "<name>", "dataCredentialHost": "<host>" }`.

## Key-Password Provider

`proton-drive-cli login` derives a key password during browser sign-in and
stores it, keyed by the session UID, under
`proton-drive-key.proton-lfs-cli.local` in the chosen provider. The CLI
creates and verifies that entry itself; it is never created by hand. The
provider is chosen by `--key-password-provider`, then
`PROTON_KEY_PASSWORD_PROVIDER`, then the default `pass-cli`. The tray stores
the choice as `keyPasswordProvider` in `~/.proton-lfs/config.json`
([Config File](../reference/config-file.md)) and passes it to `login` and
`doctor`.

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

## Mailbox/Data Password (two-password accounts)

Browser-fork sessions with a stored key password unlock Drive without a
separate data credential, also for accounts Proton reports as two-password
mode. Configure one only when `proton-drive-cli doctor` or
`bridge auth-state` reports `needs_data_password`. The entry lives under the
separate host `proton-data.proton-lfs-cli.local`:

```bash
printf "protocol=https\nhost=proton-data.proton-lfs-cli.local\nusername=user@proton.me\npassword=<mailbox-data-password>\n\n" | git credential approve
git config lfs.customtransfer.proton.args "--backend sdk --data-credential-provider git-credential"
```

For Proton Pass, create a login item with URL
`https://proton-data.proton-lfs-cli.local` and use
`--data-credential-provider pass-cli` or
`PROTON_DATA_CREDENTIAL_PROVIDER=pass-cli`. Without a configured provider,
a session that needs the data password fails with `data_password_required`;
the adapter never reuses account-login material and never logs in from a
transfer.

Check the configuration offline before a transfer or a live canary:

```bash
proton-drive-cli doctor --key-password-provider git-credential --data-credential-provider git-credential
```

The doctor checks the credential entries, the session file's permissions and
shape, secret environment variables and the bridge entry point without
contacting Proton (`submodules/proton-drive-cli/docs/reference/doctor.md`).
A `ready` result is a local readiness statement, not a remote session
validation: a session Proton revoked server-side fails at the first SDK call
with an auth error and is never retried with a login.

## Session Refresh

The tray refresh heartbeat is token maintenance only. It calls
`proton-drive-cli session refresh --json` 10 minutes before `tokenExpiresAt`
(every 12 hours when no expiry is recorded); the result preserves redacted
HTTP and Proton API details without printing token values. Recoverable
failures and timeouts are retried after one minute. Non-recoverable failures,
including Proton `10013` `Invalid refresh token`, are remembered for the same
saved session and shown as `Refresh: Reconnect required` /
`Transfers: Reconnect required`; the tray does not keep retrying and does not
start login automatically.

A successful browser-fork Connect creates a new session and clears the
same-session refresh blocker.

## Helper Script

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

The script verifies with `pass-cli info` that pass-cli is authenticated and
exports `PROTON_PASS_CLI_BIN`. It also unsets the former account-credential
reference variables; proton-drive-cli searches provider vaults directly for
the key-password and optional data-password entries.

## proton-drive-cli App Identity

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_APP_VERSION` | empty | Optional Proton `x-pm-appversion` override forwarded by the Go adapter as the bridge `appVersion` field; must follow the grammar below or proton-drive-cli rejects the request before any network call |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@<cli version>` | proton-drive-cli's default Proton app-version header (version part follows its `package.json`) |

Proton's SDK README requires third-party clients to identify as
`external-drive-{name}@{major.minor.patch}[-stable|beta|alpha][+build]`, where
`{name}` uses lowercase letters and underscores. The official `cli-drive`
identity is reserved for Proton's own CLI and is rejected by proton-drive-cli.
The Go default `DefaultDriveCLIAppVersion` must equal proton-drive-cli's
default; `internal/config/app_version_test.go` checks it against the
submodule's `package.json` version.

## Redacted Scope Diagnostics

Use the helper diagnostics command when local readiness is green but Proton
rejects the saved session during the live metadata gate:

```bash
proton-lfs-tray scope-diagnostics
```

This local mode prints JSON evidence only: credential-provider selections,
session-file shape, redacted session identifiers, token presence flags, token
expiry, local scope names, app-version inputs, and the offline
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
