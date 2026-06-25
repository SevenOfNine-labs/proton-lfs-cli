# Adapter Configuration Reference

Source of truth in code: `cmd/adapter/config_constants.go`.

## Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_LFS_BACKEND` | `local` | Adapter backend (`local`, `sdk`) |
| `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enables mock transfer mode |
| `PROTON_LFS_LOCAL_STORE_DIR` | empty | Local backend object root |
| `PROTON_CREDENTIAL_PROVIDER` | `pass-cli` | Legacy compatibility setting; ignored by transfer bridge requests |
| `PROTON_DATA_CREDENTIAL_PROVIDER` | empty | Optional separate provider for explicit mailbox/data unlock fallback |
| `PROTON_DATA_CREDENTIAL_HOST` | `proton-data.proton-lfs-cli.local` | Credential host/key for the mailbox/data password entry |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path (passed through to proton-drive-cli) |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |

The Go adapter does **not** resolve Proton account credentials and never sends
account username/password selectors to proton-drive-cli. Account login is
browser-fork-only and must be completed outside Git LFS transfers with
`proton-drive login --key-password-provider <provider>`. Transfer bridge
requests send only local unlock selectors such as
`{ "dataCredentialProvider": "<name>", "dataCredentialHost": "<host>" }` for
legacy sessions or explicit `needs_data_password` recovery.

### pass-cli (default)

For browser-fork login, use `proton-drive login --key-password-provider pass-cli`
to store the derived session key password in Proton Pass. That key-password
entry is the normal Drive unlock path for browser-fork sessions. If
`proton-drive doctor` explicitly reports `needs_data_password`, create a second
login item whose URL is `https://proton-data.proton-lfs-cli.local`, then enable
`PROTON_DATA_CREDENTIAL_PROVIDER=pass-cli` or pass
`--data-credential-provider pass-cli`.

The tray Connect action does not prompt for Proton username/password when
`pass-cli` lookup fails. Fix the Proton Pass item instead: it must be a login
item with a URL matching `https://proton.me` and populated username/email plus
password fields.

The `PROTON_PASS_CLI_BIN` env var is forwarded to the subprocess via the env allowlist.

### git-credential

When `PROTON_KEY_PASSWORD_PROVIDER=git-credential` or
`--key-password-provider git-credential` is used, proton-drive-cli stores the
browser-fork key password via `git credential approve`.

If `proton-drive doctor` reports `needs_data_password`, store the mailbox/data
password under the separate data host:

```bash
printf "protocol=https\nhost=proton-data.proton-lfs-cli.local\nusername=user@proton.me\npassword=<mailbox-data-password>\n\n" | git credential approve
git config lfs.customtransfer.proton.args "--backend sdk --data-credential-provider git-credential"
```

Browser-fork sessions with stored key-password material do not need this
separate data credential. If no required data credential provider is
configured, affected legacy unlock paths fail with `DATA_PASSWORD_REQUIRED`.
The adapter intentionally does not reuse account-login material and does not
attempt account login from transfers.

Before attempting a real login or SDK transfer, run:

```bash
proton-drive doctor --key-password-provider git-credential --data-credential-provider git-credential
```

This preflight is offline: it checks local credential entries, session-file
permissions/shape, stale secret environment variables, and the bridge entry
point without attempting Proton authentication. A ready result is therefore a
local readiness statement, not a remote session validation. If Proton has
revoked or expired the browser session server-side, the next guarded live
metadata or transfer call must surface that as an auth failure without starting
a new login attempt.

## Session Refresh

The tray refresh heartbeat is token maintenance only. It calls
`proton-drive-cli session refresh --json`, which preserves redacted HTTP and
Proton API details without printing token values. Recoverable failures retry
after a short backoff. Non-recoverable failures, including Proton `10013`
`Invalid refresh token`, are remembered for the same saved session and shown as
`Refresh: Reconnect required` / `Transfers: Reconnect required`; the tray does
not keep retrying and does not start login automatically.

A successful browser-fork Connect creates a new session and clears the
same-session refresh blocker.

## Helper Script

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

The script verifies that `pass-cli` is authenticated and sets `PROTON_PASS_CLI_BIN`.
It also unsets legacy account credential reference variables; proton-drive-cli
searches provider vaults directly for browser-fork key-password and optional
data-password entries.

## proton-drive-cli Constants

The Go adapter spawns `proton-drive-cli bridge <command>` directly as a subprocess:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_APP_VERSION` | empty | Optional Proton client app version override forwarded by the Go adapter |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-protonlfscli@0.1.2` | proton-drive-cli default Proton app-version header |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |
| `PROTON_DRIVE_CLI_TIMEOUT_MS` | `300000` | Subprocess command timeout |
| `PROTON_DRIVE_CLI_SESSION_DIR` | `~/.proton-drive-cli` | Session file storage directory |

Two-factor challenges are handled by interactive `proton-drive login`. Transfer commands surface `TWO_FACTOR_REQUIRED` and stop instead of attempting repeated logins.

## Redacted Scope Diagnostics

Use the helper diagnostics command when local readiness is green but Proton
rejects the saved session during the live metadata gate:

```bash
proton-lfs-cli scope-diagnostics
```

This local mode prints JSON evidence only: credential-provider selections,
session-file shape, redacted session identifiers, token presence flags, token
expiry, local scope names, app-version inputs, and the offline
`bridge auth-state` result. It does not contact Proton.

To perform the one read-only server probe, use the same explicit live canary
acknowledgement:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
  proton-lfs-cli scope-diagnostics --live
```

Live mode adds exactly one `bridge list` request for `/` through
`https://drive-api.proton.me` and reports structured Proton errors such as
`INSUFFICIENT_SCOPE` / API 9101. It does not run login, init, upload, download,
delete, refresh, or retry loops, and it omits raw token, password, UID, and
session values. If local `authState.state` is not `ready`, live mode stops
before the read-only server probe even when the acknowledgement is present.
