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
allowlist); the credential store variables are read only by
`proton-drive-cli login` and are not forwarded:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@<cli version>` | proton-drive-cli's own app-version default |
| `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID` | unset | Redacted `[AUTH_TRACE]` lines on stderr and their correlation id; the tray sets both during Connect |
| `PROTON_CREDENTIAL_STORE` | `pass-cli` | Credential store that `login` saves the session tokens and key password in |
| `PROTON_CREDENTIAL_HOST` | `proton-drive-cli.invalid` | Credential host of that entry |

The Go adapter does **not** resolve Proton credentials and never sends
account or credential fields to proton-drive-cli. Account login is
browser-fork-only and happens outside Git LFS transfers with
`proton-drive-cli login --credential-store <store>`. Transfer bridge
requests carry only `storageBase`, `appVersion` (when set) and the command
fields (`oid`, `path`, `outputPath`, `oids`).

## Credential Store

`proton-drive-cli login` receives the session tokens and the key password
from the browser session fork, for every account type, and stores them as
one entry (the JSON object `{uid, revision, previous, accessToken,
refreshToken, keyPassword}`), keyed by the session UID, under
`proton-drive-cli.invalid` in the chosen
credential store. `~/.proton-drive-cli/session.json` records the UID,
scopes, store, host, token expiry and a revision that changes with every
write; it holds no secret. The CLI creates, verifies and rewrites the entry
itself (every token refresh replaces it); it is never created by hand. The
store is chosen by `--credential-store`, then `PROTON_CREDENTIAL_STORE`, then
the default `pass-cli`, and recorded in the session; every later command
(bridge, doctor, logout, session refresh) uses the recorded store and host.
The tray stores the choice as `credentialStore` in
`~/.proton-lfs/config.json` ([Config File](../reference/config-file.md)) and
passes it to `login`.

### pass-cli (default)

```bash
pass-cli login
proton-drive-cli login --credential-store pass-cli
```

The entry appears in Proton Pass as a login item titled
`Proton proton-drive-cli.invalid` with URL `https://proton-drive-cli.invalid`;
every write creates a new item and then deletes the items it replaces. When
the lookup fails (`credentials_unavailable`), run `pass-cli login` and then
`proton-drive-cli login` again with the same store; the tray's Connect does
not prompt for account credentials. `PROTON_PASS_CLI_BIN` names a non-default pass-cli binary; the
tray sets it to the binary it finds on `PATH`, `~/.local/bin`,
`/opt/homebrew/bin` or `/usr/local/bin`.

### git-credential

```bash
proton-drive-cli login --credential-store git-credential
```

proton-drive-cli stores the entry with `git credential approve` and reads it
back with `git credential fill` for host `proton-drive-cli.invalid`; a
helper that kept the existing item gets `reject` and `approve`
(git-credential(1)). The git-credential store needs git 2.26 or newer
(`git config --show-scope`). It runs only the first helper of the system
and global git configuration (git-config(1) SCOPES), from
`credential.helper` or `credential.https://proton-drive-cli.invalid.helper`,
that encrypts secrets at rest, pinned with
`git -c credential.helper= -c credential.helper=<helper>`:

| Helper | Secret store | Condition |
| --- | --- | --- |
| `osxkeychain` | macOS Keychain | none |
| `wincred` | Windows Credential Manager | none |
| `libsecret` | freedesktop Secret Service (GNOME Keyring, KWallet) | none |
| `manager` | Git Credential Manager | `GCM_CREDENTIAL_STORE` or `credential.credentialStore` is `keychain`, `gpg` or unset on macOS, `wincredman`, `dpapi` or unset on Windows, `secretservice` or `gpg` elsewhere; the validated store is pinned through `GCM_CREDENTIAL_STORE` |

`store`, `cache` and shell (`!`) helpers are never run; without an allowed
helper the store fails. git and Git Credential Manager do not prompt for
credentials (`GIT_TERMINAL_PROMPT=0`, `credential.interactive=false`,
`GCM_INTERACTIVE=false`, `GCM_GUI_PROMPT=false`, `GCM_PROVIDER=generic`); the
operating system may still ask to unlock a locked keychain or keyring.

### Lifecycle of the entry

- Every token refresh rewrites it under the session lock and reads it back;
  a failed save is retried three times.
- `proton-drive-cli logout` revokes the session, deletes the session file
  and crypto cache, then removes the entry, and warns when it cannot.
- A login that yields a new session UID removes the entry of the previous UID.
- A refresh token the API rejects with a Proton error body and HTTP 4xx
  other than 408 and 429, while no other process has rotated it, signs the
  session out: `session.json`, the crypto cache and the entry are deleted,
  and `auth-state` reports `needs_login`.
- An entry the store cannot return, or a key password that does not
  decrypt the account keys, makes transfers fail with
  `credentials_unavailable`; fix the store or run `proton-drive-cli login`.

## Offline Doctor

Check the configuration offline before a transfer or a live canary:

```bash
proton-drive-cli doctor
proton-drive-cli doctor --credential-store git-credential --json
```

Without options the doctor verifies the store and host recorded in the
session; `--credential-store` and `--credential-host` verify another entry.
It checks the bridge entry point (`drive-cli-bin`), the UID-scoped
credential-store entry (`session-credentials`) and the session file's
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

This local mode prints JSON evidence only: the credential store selection,
session-file presence and mode, a UID fingerprint, the session's credential
store and host, token expiry, local scope names, app-version inputs, and the
offline `bridge auth-state` result. It does not contact Proton.

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
