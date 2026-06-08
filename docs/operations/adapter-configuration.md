# Adapter Configuration Reference

Source of truth in code: `cmd/adapter/config_constants.go`.

## Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_LFS_BACKEND` | `local` | Adapter backend (`local`, `sdk`) |
| `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enables mock transfer mode |
| `PROTON_LFS_LOCAL_STORE_DIR` | empty | Local backend object root |
| `PROTON_CREDENTIAL_PROVIDER` | `pass-cli` | Credential provider: `pass-cli` (default) or `git-credential` |
| `PROTON_DATA_CREDENTIAL_PROVIDER` | empty | Optional separate provider for a two-password account mailbox/data password |
| `PROTON_DATA_CREDENTIAL_HOST` | `proton-data.proton-lfs-cli.local` | Credential host/key for the mailbox/data password entry |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path (passed through to proton-drive-cli) |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |

The Go adapter does **not** resolve credentials itself. It sends non-secret selectors such as `{ "credentialProvider": "<name>", "dataCredentialProvider": "<name>", "dataCredentialHost": "<host>" }` to proton-drive-cli, which handles credential resolution internally. Password values are never passed as command-line arguments.

### pass-cli (default)

`proton-drive-cli` searches all Proton Pass vaults for a login item with a `proton.me` URL. For a separate mailbox/data password, create a second login item whose URL is `https://proton-data.proton-lfs-cli.local`, then enable `PROTON_DATA_CREDENTIAL_PROVIDER=pass-cli` or pass `--data-credential-provider pass-cli`.

The `PROTON_PASS_CLI_BIN` env var is forwarded to the subprocess via the env allowlist.

### git-credential

When `PROTON_CREDENTIAL_PROVIDER=git-credential`, proton-drive-cli resolves credentials via `git credential fill`.

For two-password accounts, store the mailbox/data password under the separate data host:

```bash
printf "protocol=https\nhost=proton-data.proton-lfs-cli.local\nusername=user@proton.me\npassword=<mailbox-data-password>\n\n" | git credential approve
git config lfs.customtransfer.proton.args "--backend sdk --credential-provider git-credential --data-credential-provider git-credential"
```

If no data credential provider is configured, two-password accounts fail with `DATA_PASSWORD_REQUIRED`. The adapter intentionally does not reuse the login password and does not keep retrying account login.

Before attempting a real login or SDK transfer, run:

```bash
proton-drive doctor --credential-provider git-credential --data-credential-provider git-credential
```

This preflight is offline: it checks local credential entries, session-file
permissions/shape, stale secret environment variables, and the bridge entry
point without attempting Proton authentication.

## Helper Script

```bash
pass-cli login
eval "$(./scripts/export-pass-env.sh)"
```

The script verifies that `pass-cli` is authenticated and sets `PROTON_PASS_CLI_BIN`.

## proton-drive-cli Constants

The Go adapter spawns `proton-drive-cli bridge <command>` directly as a subprocess:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_APP_VERSION` | empty | Optional Proton client app version override forwarded by the Go adapter |
| `PROTON_DRIVE_CLI_APP_VERSION` | `external-drive-proton-lfs-cli@0.1.2` | proton-drive-cli default Proton app-version header |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |
| `PROTON_DRIVE_CLI_TIMEOUT_MS` | `300000` | Subprocess command timeout |
| `PROTON_DRIVE_CLI_SESSION_DIR` | `~/.proton-drive-cli` | Session file storage directory |

Two-factor challenges are handled by interactive `proton-drive login`. Transfer commands surface `TWO_FACTOR_REQUIRED` and stop instead of attempting repeated logins.
