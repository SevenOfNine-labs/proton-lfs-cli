# Subprocess Integration

The adapter's `sdk` backend talks to `proton-drive-cli` through
`BridgeClient` (`cmd/adapter/bridge.go`): one short-lived subprocess per
bridge command, a JSON request on stdin, a JSON envelope on stdout. This page
describes that client; the command vocabulary, request fields and payloads
are specified by proton-drive-cli in `schemas/bridge/v1/` and its bridge
protocol reference (`submodules/proton-drive-cli/docs/reference/bridge-protocol.md`,
published on the [proton-drive-cli documentation site](https://sevenofnine-labs.github.io/proton-drive-cli/)).

## Why a subprocess

- Process isolation: a crash or hang inside the Node.js runtime ends one
  command, not the adapter, and is reported as a per-object error.
- One distribution model: the same `proton-drive-cli` binary serves users on
  the command line, the tray and the adapter.
- Language boundary: the adapter stays pure Go (`CGO_ENABLED=0`), the Drive
  SDK stays TypeScript.
- Concurrency comes from git-lfs, which runs one adapter process per transfer
  slot; the adapter needs no pool, queue or semaphore.

## Program selection

`--drive-cli-bin` (or `PROTON_DRIVE_CLI_BIN`, default
`submodules/proton-drive-cli/dist/index.js`) names the entry point:

| Entry point | argv |
| --- | --- |
| ends in `.js`, `.cjs` or `.mjs` | `<node> <entry> bridge <command>`, where `<node>` is `NODE_BIN` or `node` found on `PATH` |
| anything else (the single-executable `proton-drive-cli`) | `<entry> bridge <command>` |

## Lifecycle of one command

```text
git-lfs-proton-adapter                         proton-drive-cli
    |                                                |
    |-- exec <argv>, own process group ------------->|
    |-- stdin: {"oid":"...","path":"...",...} ------>|  (stdin closed after the write)
    |                                                |-- validate fields against request-field-rules.json
    |                                                |-- run the command (SDK, providers)
    |                                                |-- stdout: one JSON envelope line
    |                                                |-- stderr: logs (ERROR level unless DEBUG=true)
    |<-- wait for exit (WaitDelay 5 s) --------------|
    |-- parse the envelope strictly                  |
    |-- map ok=false or transport failure to a BackendError
```

- The request is the JSON object built by `baseRequest` plus the command's
  own fields: `storageBase` (always), `appVersion` (when
  `PROTON_APP_VERSION` is set), and `oid`, `path` or `outputPath` as the
  command needs. No other field is ever sent;
  `cmd/adapter/bridge_contract_test.go` checks every request shape against
  proton-drive-cli's `request-field-rules.json`.
- stdout is parsed as the whole output first; if that is not a valid
  envelope, the last line that is a JSON object is tried, so a dependency
  that prints a runtime warning to stdout does not break the adapter. Any
  JSON that is not a valid envelope is an error, never a success.
- The envelope is validated strictly: `ok` must be a boolean, the only
  allowed keys are `ok`, `payload`, `error`, `code`, `details`; a success
  must carry no `error`, `code` or `details`; a failure must carry a
  non-empty `error`, a positive `code` and no `payload`.
- Success payloads of `exists` are parsed strictly as well (unknown fields
  are errors).

## Timeout and termination

| Aspect | Rule |
| --- | --- |
| Base budget | `--bridge-timeout` / `PROTON_LFS_BRIDGE_TIMEOUT`, default 5 minutes |
| Transfer budget | base plus one second per 256 KiB of the size git-lfs announced (`upload`, `download`) |
| Expiry | The whole process group is killed with `SIGKILL` (Unix), so helpers the bridge spawned (git credential, pass-cli) do not outlive it; on Windows the process is killed |
| Pipe wait | `WaitDelay` 5 s bounds the wait for stdout/stderr after the process is gone |
| Size cap | the size-derived budget never exceeds 24 hours |
| Reported as | `bridge <command> timed out after <duration>; raise --bridge-timeout or PROTON_LFS_BRIDGE_TIMEOUT for large objects or slow links`, classified as 504 `network_failure`, retryable |
| Adapter interrupted | SIGINT or SIGTERM to the adapter kills the running bridge (and its process group on Unix) before the adapter exits with 130 or 143 |

Inside the bridge, the stdin read is bounded at 30 seconds, each request of
proton-drive-cli's own API client at 30 seconds, and SDK requests use the
timeout the SDK sets per request; the adapter's budget is the outer bound.

## Environment

The subprocess receives only the allowlisted variables listed in
[Adapter Reference](../reference/adapter.md#environment-forwarded-to-proton-drive-cli):
`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `LC_*`, `TERM`, `NODE_ENV`,
`NODE_OPTIONS`, `NODE_PATH`, `NODE_BIN`, the four `XDG_*` base directories,
`TMPDIR`/`TMP`/`TEMP`, the Windows profile and system variables,
`MOCK_BRIDGE_*`, the `PROTON_` variables bridge commands read
(`PROTON_DRIVE_CLI_APP_VERSION`, `PROTON_PASS_CLI_BIN`, `PROTON_AUTH_TRACE`,
`PROTON_AUTH_TRACE_ID`), and the configuration of the credential tools they
start (`PROTON_PASS_SESSION_DIR`, `PROTON_PASS_KEY_PROVIDER`,
`PROTON_PASS_ENCRYPTION_KEY`, `PROTON_PASS_LINUX_KEYRING`,
`DBUS_SESSION_BUS_ADDRESS`, `GIT_CONFIG_GLOBAL`, `GCM_*`). The key-password
provider and host come from the saved session, so `PROTON_KEY_PASSWORD_*` is
not forwarded. The only secret forwarded is `PROTON_PASS_ENCRYPTION_KEY`,
which pass-cli's `env` key provider reads.

## Error mapping

| Situation | BackendError |
| --- | --- |
| `ok: false` envelope with `details.errorCode` | Root code from `schemas/bridge/v1/error-code-map.json`, HTTP-style code from the envelope |
| `ok: false` envelope with an `errorCode` outside the map | The envelope `code` |
| Spawn failure (`BridgeStartError`) | 400 `invalid_request`, not retryable |
| Timeout (`BridgeTimeoutError`) | 504 `network_failure`, retryable |
| Non-zero exit with empty or malformed stdout | Keywords in the client message; stderr is redacted (`bridgeproto.RedactText`) and then cut to 256 runes before it is quoted |

The complete tables are in [Adapter Errors](../reference/adapter-errors.md).

## Commands the adapter uses

| Adapter method | Bridge command | When |
| --- | --- | --- |
| `AuthState` | `auth-state` | `init`; refuses any state but `ready` and `session_expired`, and any payload with `willAttemptNetwork: true` |
| `InitLFSStorage` | `init` | `init`, after `auth-state` |
| `Exists` | `exists` | before every upload; a 404 envelope means absent |
| `Upload` | `upload` | upload of an object that does not exist yet |
| `Download` | `download` | download into a fresh temporary file |

`list` and `delete` are not called by the adapter; the tray's
`scope-diagnostics --live` uses `list`, and the real E2E test uses `delete`
for cleanup.

## auth-state payload

`bridge auth-state` (`schemas/bridge/v1/auth-state-payload.schema.json`,
parsed into `BridgeAuthStateResponse`) answers with `state`, `hasSession`,
`sessionValid`, `sessionExpired`, `sessionUidPresent`,
`keyPasswordAvailable`, `keyPasswordProvider`, `keyPasswordHost`,
`willAttemptNetwork` (always false), `errors` and `actions`. It reads only
the session file and the session's key-password entry, sends no request to
Proton and never refreshes tokens. The first state that applies is
reported:

| State | Condition |
| --- | --- |
| `needs_login` | No session file |
| `session_invalid` | The session file is unreadable, lacks `uid`, `accessToken`, `refreshToken`, `keyPasswordProvider` or `keyPasswordHost`, has a blank one, or names a provider other than `pass-cli` or `git-credential`: any file not written by `proton-drive-cli login` |
| `session_expired` | `tokenExpiresAt` is in the past |
| `needs_key_password` | The key-password entry cannot be read from the session's provider |
| `ready` | Otherwise |

The adapter's classification of each state is in
[Adapter Errors](../reference/adapter-errors.md#raised-by-the-sdk-backend-from-a-bridge-auth-state).

## Tests

- `cmd/adapter/bridge_test.go`: argv selection, environment filtering,
  timeout scaling, strict envelope parsing, stderr redaction, payload parsing.
- `cmd/adapter/bridge_contract_test.go`: request shapes against
  `request-field-rules.json`, auth states against
  `auth-state-payload.schema.json`, error codes against `error-code-map.json`.
- `tests/integration/git_lfs_sdk_backend_test.go` and
  `tests/integration/git_lfs_e2e_mock_test.go`: the whole path through
  `tests/testdata/mock-proton-drive-cli.js`.
