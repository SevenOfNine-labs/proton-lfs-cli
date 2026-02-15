# Proton SDK Bridge

Bridge implementation: the Go adapter (`cmd/adapter/bridge.go`) spawns `proton-drive-cli bridge <command>` as a subprocess.

## Architecture

```
Go Adapter → proton-drive-cli subprocess (JSON stdin/stdout, credentialProvider field) → Proton API
                    ↓
        pass-cli or git-credential
    (resolved internally by proton-drive-cli)
```

The adapter's `BridgeClient` spawns `proton-drive-cli bridge <command>` as a subprocess, passing JSON via stdin and reading JSON from stdout. The Go adapter sends only a `credentialProvider` name — proton-drive-cli resolves actual credentials internally. Credentials are never passed via command-line arguments.

## Subprocess Communication Protocol

The adapter (`cmd/adapter/bridge.go`) communicates with `proton-drive-cli` using:

1. **Spawn**: `node <proton-drive-cli-path> bridge <command>`
2. **Stdin**: JSON payload with credentials and operation parameters
3. **Stdout**: JSON response envelope `{ ok: true/false, payload: {...}, error: "...", code: 400-500 }`
4. **Stderr**: Diagnostic logs (not parsed for responses)

## Bridge Commands

- `init`: Authenticate with Proton API using provided credentials.
- `upload`: Upload a file to Proton Drive by OID.
- `download`: Download a file from Proton Drive by OID.
- `list`: List files in a Proton Drive folder.
- `refresh`: Refresh an existing session token.

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

1. proton-drive-cli session refresh not fully reliable (workaround: re-authenticate on 401).
2. CAPTCHA may require manual intervention for new accounts.
3. No streaming for large files (>2GB may timeout -- increase `PROTON_DRIVE_CLI_TIMEOUT_MS`).

## Next Hardening Targets

1. Improve session reuse to avoid re-authentication on every operation.
2. Add strict response schema validation between adapter and subprocess.
3. Add fault-injection tests (timeouts, partial writes, session expiry).
4. Address upstream session refresh issue in proton-drive-cli.
