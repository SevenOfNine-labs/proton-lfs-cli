# Proton SDK Bridge

Bridge implementation: the Go adapter (`cmd/adapter/bridge.go`) spawns `proton-drive-cli bridge <command>` as a subprocess.

## Architecture

```
Go Adapter -> proton-drive-cli subprocess (JSON stdin/stdout)
             account session must already exist from browser-fork login
```

The adapter's `BridgeClient` spawns `proton-drive-cli bridge <command>` as a
subprocess, passing JSON via stdin and reading JSON from stdout. The Go adapter
never sends account username/password fields, credential selectors, or login
permission flags: every request carries only `storageBase`, `appVersion`
(when set) and the command's own fields. The bridge unlocks Drive with the
key password the saved session's provider holds; the browser fork delivers
that key password for every account type.

## Subprocess Communication Protocol

The adapter (`cmd/adapter/bridge.go`) communicates with `proton-drive-cli` using:

1. **Spawn**: `node <dist/index.js> bridge <command>` for a `.js` entry point, or `<proton-drive-cli> bridge <command>` for the standalone executable (`--drive-cli-bin`)
2. **Stdin**: JSON request with the storage base, the optional app version and the operation parameters
3. **Stdout**: one JSON response envelope line, `{ ok: true, payload }` or `{ ok: false, error, code, details }`
4. **Stderr**: Diagnostic logs (not parsed for responses)

The Go adapter validates the top-level response envelope strictly. `ok` must
be a boolean, success responses cannot include error fields, failed responses
must include a non-empty `error` string and positive `code`, and unknown
top-level fields are rejected. proton-drive-cli validates request fields against
`schemas/bridge/v1/request-field-rules.json` before command handlers run, and
the root adapter has contract tests proving the fields it sends remain allowed
and complete. Root command-specific success parsers also fail closed for
malformed `exists` and batch helper payloads instead of inferring success.

## Bridge Commands

- `auth-state`: Inspect local auth/session readiness without any Proton
  request; it reads the saved session and checks that the session's
  key-password entry is readable from its provider.
- `init`: Ensure the configured LFS storage directory exists.
- `upload`: Upload a file to Proton Drive by OID.
- `download`: Download a file from Proton Drive by OID.
- `exists`: Check whether a file exists by OID.
- `delete`: Delete a file by OID.
- `list`: List files in a Proton Drive folder (used by `proton-lfs-tray scope-diagnostics --live`, not by transfers).
- `batch-exists`, `batch-delete`: Check or delete multiple OIDs. The adapter
  sends neither; they are proton-drive-cli commands for direct use.

## Security Considerations

- No account credentials are accepted by bridge transfer commands
- `auth-state` readiness is local-only. It validates the saved session's
  shape, its local expiry and the presence of its key-password entry; it
  cannot prove a remotely revoked Proton session is still accepted by the
  server.
- Bridge requests are passed via stdin (not visible in `ps` output) and carry
  no credentials
- OID validation: strict 64-character hex regex before subprocess spawn
- Path traversal prevention: reject paths containing `..`
- One subprocess per command; timeout `--bridge-timeout` / `PROTON_LFS_BRIDGE_TIMEOUT` (default 5 minutes) plus one second per 256 KiB for transfers, with the process group killed on expiry
- Session tokens stored in `~/.proton-drive-cli/session.json` with 0600 permissions

The complete request, payload and error-code tables are in proton-drive-cli's
bridge protocol reference (`submodules/proton-drive-cli/docs/reference/bridge-protocol.md`,
published on the [proton-drive-cli documentation site](https://sevenofnine-labs.github.io/proton-drive-cli/));
the Go client is described in [Subprocess Integration](subprocess-integration.md).

## Requirements Propagated From Git LFS

- Upload/download must preserve exact bytes and object identity.
- Errors must be typed and per-object, not process-fatal.
- Session failure must produce explicit adapter errors, never silent success.
- API contracts must remain deterministic so adapter tests can assert behavior.
- Subprocess failures must not leak secrets: malformed output, timeouts, and
  stderr diagnostics are covered by mocked bridge tests.
- Command-specific bridge request fields must stay aligned with
  `request-field-rules.json`; adding a required field or disallowing a root
  field must fail root contract tests before runtime.

## Known Issues

1. CAPTCHA may require manual intervention for new accounts.
2. Two-factor authentication, FIDO2 included, is completed in the browser
   during `proton-drive-cli login`, never in the transfer path.
3. Remote session revocation can still make the first live SDK call fail after
   offline `ready`; this must be reported as an auth failure, not retried as an
   automatic login.
4. The sdk backend reports progress once per object after the bridge command returns; the bridge timeout grows with the object size (`--bridge-timeout` plus one second per 256 KiB), and `--bridge-timeout` raises the base.

## Next Hardening Targets

1. Extend fault-injection tests beyond timeout, partial-output, and
   session-expiry coverage as new SDK failure modes are identified.
2. Add a real-account canary only after the mocked auth gates stay green.
