# Adapter Configuration Reference

Source of truth in code: `cmd/adapter/config_constants.go`.

## Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_LFS_BACKEND` | `local` | Adapter backend (`local`, `sdk`) |
| `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Enables mock transfer mode |
| `PROTON_LFS_LOCAL_STORE_DIR` | empty | Local backend object root |
| `PROTON_CREDENTIAL_PROVIDER` | `pass-cli` | Credential provider: `pass-cli` (default) or `git-credential` |
| `PROTON_PASS_CLI_BIN` | `pass-cli` | Proton Pass CLI binary path (passed through to proton-drive-cli) |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |

The Go adapter does **not** resolve credentials itself. It sends `{ "credentialProvider": "<name>" }` to proton-drive-cli, which handles all credential resolution internally.

### pass-cli (default)

`proton-drive-cli` searches all Proton Pass vaults for a login item with a `proton.me` URL. The `PROTON_PASS_CLI_BIN` env var is forwarded to the subprocess via the env allowlist.

### git-credential

When `PROTON_CREDENTIAL_PROVIDER=git-credential`, proton-drive-cli resolves credentials via `git credential fill`.

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
| `PROTON_APP_VERSION` | `external-drive-protonlfs@dev` | Proton client app version header |
| `PROTON_DATA_PASSWORD` | empty | Optional dedicated data password fallback |
| `PROTON_SECOND_FACTOR_CODE` | empty | Optional 2FA code fallback |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to proton-drive-cli entry point |
| `PROTON_DRIVE_CLI_TIMEOUT_MS` | `300000` | Subprocess command timeout |
| `PROTON_DRIVE_CLI_SESSION_DIR` | `~/.proton-drive-cli` | Session file storage directory |
