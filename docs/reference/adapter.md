# Adapter Reference

`git-lfs-proton-adapter` is the Git LFS custom transfer agent. git-lfs starts
it (one process per concurrent transfer slot), speaks the custom transfer
protocol over its stdin/stdout, and stops it with `terminate` or by closing
stdin. It is not run by hand; `--help` prints the full usage text.

Source of truth: `cmd/adapter/main.go` (flags, usage), `cmd/adapter/bridge.go`
(subprocess client), `internal/config/config.go` (environment names and
defaults).

## Registration in git

```bash
git config lfs.customtransfer.proton.path /path/to/git-lfs-proton-adapter
git config lfs.customtransfer.proton.args "--backend sdk --drive-cli-bin /path/to/proton-drive-cli"
git config lfs.standalonetransferagent proton
```

`proton-lfs-tray register` (or the tray's "Enable LFS Backend" item) writes
the same three keys with `--global`; see [Tray Reference](tray.md).

## Flags

Every environment variable in the table is read as the flag's default; a flag
given on the command line wins.

| Flag | Environment variable | Default | Meaning |
| --- | --- | --- | --- |
| `--backend` | `PROTON_LFS_BACKEND` | `local` | `local` (filesystem object store) or `sdk` (Proton Drive through `proton-drive-cli bridge`). Any other value exits with status 2. |
| `--drive-cli-bin` | `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | proton-drive-cli entry point. A `.js`, `.cjs` or `.mjs` file runs under node; anything else (the single-executable build `proton-drive-cli`) runs directly. |
| `--local-store-dir` | `PROTON_LFS_LOCAL_STORE_DIR` | empty | Object root of the `local` backend. `init` fails with code 501 when it is empty. |
| `--allow-mock-transfers` | `ADAPTER_ALLOW_MOCK_TRANSFERS` | `false` | Simulate uploads and downloads without any backend (protocol tests only). |
| `--data-credential-provider` | `PROTON_DATA_CREDENTIAL_PROVIDER` | empty | Provider holding the mailbox/data password of a two-password account: `pass-cli` or `git-credential`. Sent to the bridge as `dataCredentialProvider`. |
| `--data-credential-host` | `PROTON_DATA_CREDENTIAL_HOST` | `proton-data.proton-lfs-cli.local` | Host/key under which that password is stored. Sent as `dataCredentialHost` only when a provider is set. |
| `--bridge-timeout` | `PROTON_LFS_BRIDGE_TIMEOUT` | `5m` | Base wall-clock budget for one bridge command (Go duration such as `10m`). `upload` and `download` add one second per 256 KiB of the object size announced by git-lfs. A non-positive or unparsable value exits with status 2. |
| `--debug` | none | `false` | Per-message diagnostics on stderr. Errors always reach stderr, which git-lfs records under `GIT_TRACE=1`. |
| `--version` | none | none | Print `git-lfs-proton-adapter <version> (commit=<hash> build_time=<time>)` and exit 0. |

## Environment-only settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `NODE_BIN` | `node` found on `PATH` | Node.js binary used to run a `.js` entry point. |
| `LFS_STORAGE_BASE` | `LFS` | Remote base folder; objects live at `/<base>/<oid[0:2]>/<oid[2:4]>/<oid>`. Sent as `storageBase`. |
| `PROTON_APP_VERSION` | empty | Override of the Proton `x-pm-appversion` identity, sent as `appVersion`. It must follow Proton's third-party grammar: `external-drive-{name}@{major.minor.patch}`, optionally followed by a `-stable`, `-beta` or `-alpha` channel and a `+build` suffix; proton-drive-cli rejects anything else before any network call. Leave it empty to use proton-drive-cli's built-in `external-drive-protonlfscli@<version>`. |
| `PROTON_LFS_STATUS_FILE` | `~/.proton-lfs/status.json` | Where the status report is written; see [Status File](status-file.md). |

## Environment forwarded to proton-drive-cli

The bridge subprocess receives only the variables below (`envAllowlist` in
`cmd/adapter/bridge.go`); everything else in the adapter's environment is
dropped. Names ending in `_` are prefixes.

- `PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `LC_*`, `TERM`
- `NODE_ENV`, `NODE_OPTIONS`, `NODE_PATH`, `NODE_BIN`
- `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR`
- `TMPDIR`, `TMP`, `TEMP`
- `USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, `APPDATA`, `LOCALAPPDATA`, `SYSTEMROOT`, `WINDIR`, `COMSPEC`, `PATHEXT` (Windows profile and system locations)
- `MOCK_BRIDGE_*` (test stand-ins)
- `PROTON_DRIVE_CLI_APP_VERSION`
- `PROTON_KEY_PASSWORD_PROVIDER`, `PROTON_KEY_PASSWORD_HOST`
- `PROTON_DATA_CREDENTIAL_PROVIDER`, `PROTON_DATA_CREDENTIAL_HOST`
- `PROTON_PASS_CLI_BIN`
- `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID`

Secret-shaped names such as `PROTON_PASSWORD` or `PROTON_DATA_PASSWORD` are
never forwarded.

## Backends

### local

Objects are stored as plain files at
`<store-dir>/<oid[0:2]>/<oid[2:4]>/<oid>` (directories `0700`). Uploads copy
through a temporary file in the target directory and rename it into place;
downloads stage a copy in a temporary file (`git-lfs-proton-download-*` in the
system temp directory) whose path is returned to git-lfs. SHA-256 is verified
on both directions. No network, no credentials.

### sdk

`init` runs `bridge auth-state` (offline) and refuses every state other than
`ready`, then `bridge init` to create the storage base folder. `upload` runs
`bridge exists` first and skips the transfer when the object is already
stored; a non-404 failure of that check fails the upload. `download` passes a
fresh temporary file as `outputPath`, then sets its mode to `0600`. Each bridge
command is one subprocess with its own timeout; a timeout kills the process
group so helpers the bridge spawned (git credential, pass-cli) die with it.
The bridge requests never carry account credentials; see
[Subprocess Integration](../architecture/subprocess-integration.md).

## Transfer validation

Before any backend call the adapter rejects a transfer (per-object
`complete` with `error`, the process keeps running) when the session is not
initialized, `size` is negative, `oid` is not 64 hex characters, or (uploads)
`path` is empty, contains a `..` segment or a null byte. Uploads are also
rejected when the file's size or SHA-256 does not match the request;
downloads are rejected when the staged object's hash or size does not match.
The full list of codes is in [Adapter Errors](adapter-errors.md).

## Process behaviour

- stdin EOF ends the process with status 0; a message that cannot be decoded
  is answered with a protocol error and ends it with status 1.
- Invalid flags (including the removed `--credential-provider`), an invalid
  `PROTON_LFS_BRIDGE_TIMEOUT` and an unknown backend exit with status 2 before
  any message is read.
- SIGINT and SIGTERM kill a running bridge subprocess together with its
  process group (the bridge runs in its own group, so a terminal Ctrl-C does
  not reach it directly) and end the adapter with status 130 or 143.
- The size-derived bridge timeout is capped at 24 hours.
- On start, files named `git-lfs-proton-*` in the system temp directory that
  are older than 10 minutes are deleted (leftovers of an earlier crash).
- Progress is streamed in 64 KiB steps for the local backend; the sdk backend
  reports the whole object once the bridge command returns.
- The status file is updated on `init`, `terminate`, every completed transfer
  and every failed transfer; write errors are ignored.
