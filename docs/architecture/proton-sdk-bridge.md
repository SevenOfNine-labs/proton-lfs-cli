# Proton SDK Bridge

Bridge implementation: the Go adapter (`cmd/adapter/bridge.go`) spawns `proton-drive-cli bridge <command>` as a subprocess.

## Architecture

```
Go Adapter → proton-drive-cli subprocess (JSON stdin/stdout, provider selector fields) → Proton API
                    ↓
        pass-cli or git-credential
    (resolved internally by proton-drive-cli)
```

The adapter's `BridgeClient` spawns `proton-drive-cli bridge <command>` as a subprocess, passing JSON via stdin and reading JSON from stdout. The Go adapter sends only provider selector fields, such as `credentialProvider` and optional `dataCredentialProvider`/`dataCredentialHost` for two-password accounts. proton-drive-cli resolves actual credentials internally. Credentials are never passed via command-line arguments.

## Subprocess Communication Protocol

The adapter (`cmd/adapter/bridge.go`) communicates with `proton-drive-cli` using:

1. **Spawn**: `node <proton-drive-cli-path> bridge <command>`
2. **Stdin**: JSON payload with credentials and operation parameters
3. **Stdout**: JSON response envelope `{ ok: true/false, payload: {...}, error: "...", code: 400-500 }`
4. **Stderr**: Diagnostic logs (not parsed for responses)

The Go adapter validates the top-level response envelope strictly. `ok` must
be a boolean, success responses cannot include error fields, failed responses
must include a non-empty `error` string and positive `code`, and unknown
top-level fields are rejected. Payload shape is still validated by each
command-specific parser.

## Bridge Commands

- `auth`: Authenticate with Proton API using provided credentials.
- `auth-state`: Inspect local auth/session readiness without network or
  credential-provider resolution.
- `init`: Ensure the configured LFS storage directory exists.
- `upload`: Upload a file to Proton Drive by OID.
- `download`: Download a file from Proton Drive by OID.
- `exists`: Check whether a file exists by OID.
- `delete`: Delete a file by OID.
- `list`: List files in a Proton Drive folder.
- `refresh`: Refresh an existing session token.
- `batch-exists`: Check multiple OIDs. Internal helper only; not used by the
  Git LFS custom transfer loop.
- `batch-delete`: Delete multiple OIDs. Internal cleanup/maintenance helper
  only; not used by the Git LFS custom transfer loop.

## Security Considerations

- Credentials passed via stdin (not visible in `ps` output)
- OID validation: strict 64-character hex regex before subprocess spawn
- Path traversal prevention: reject paths containing `..`
- Subprocess pool: maximum 10 concurrent operations
- Timeout: 5 minutes per operation (configurable via `PROTON_DRIVE_CLI_TIMEOUT_MS`)
- Session tokens stored in `~/.proton-drive-cli/session.json` with 0600 permissions

## Requirements Propagated From Git LFS

- Upload/download must preserve exact bytes and object identity.
- Errors must be typed and per-object, not process-fatal.
- Session failure must produce explicit adapter errors, never silent success.
- API contracts must remain deterministic so adapter tests can assert behavior.

## Known Issues

1. CAPTCHA may require manual intervention for new accounts.
2. FIDO2-only 2FA is surfaced as an auth-required state and must be completed outside the transfer path.
3. No streaming for large files (>2GB may timeout -- increase `PROTON_DRIVE_CLI_TIMEOUT_MS`).

## Next Hardening Targets

1. Add command-specific payload schema validation between adapter and
   subprocess.
2. Add fault-injection tests (timeouts, partial writes, session expiry).
3. Add a real-account canary only after the mocked auth gates stay green.
