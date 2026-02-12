# Security Threat Model

## Trust Boundaries

```
[User Machine]
  ├── Git Client (trusted)
  ├── Git LFS (trusted)
  ├── Go Adapter (trusted, our code)
  ├── proton-drive-cli subprocess (trusted, our submodule)
  └── pass-cli (trusted, Proton official)

[Network Boundary]
  └── Proton API (external, TLS-protected)
```

## Attack Surfaces

### 1. Subprocess Command Injection

**Risk**: Malicious OID or file path with shell metacharacters could execute arbitrary commands.

**Mitigations**:

- `exec.CommandContext` with explicit arguments (not shell string) — no shell interpretation
- OID validated against `/^[a-f0-9]{64}$/i` before subprocess spawn
- File paths validated against `..` traversal and null bytes before use
- Credentials passed via stdin JSON, not command-line arguments
- Subprocess environment is filtered via allowlist — only PATH, HOME, NODE_*, MOCK_BRIDGE_*, etc. are forwarded

**Tests**: `cmd/adapter/bridge_test.go` (filteredEnv, matchesAllowlist tests)

### 2. Credential Exposure

**Risk**: Credentials visible in process list, logs, or on disk.

**Mitigations**:

- Credentials passed via stdin JSON to subprocess (not visible in `ps aux`)
- Credential flow: pass-cli -> Go adapter -> stdin JSON -> proton-drive-cli (memory only)
- **No HTTP layer** — credentials never traverse network connections, even localhost
- **Passwords are never persisted to disk** — `saveSession()` strips `mailboxPassword` before writing
- **Passwords are never accepted via CLI flags** — only resolved via pass-cli or git-credential
- Session file (`~/.proton-drive-cli/session.json`) contains only revocable tokens (sessionId, accessToken, refreshToken)
- Session directory `0700`, session file `0600` (owner-only)
- Error messages sanitized — no credential values in responses or logs
- Usernames are not logged (prevents email leak to log files)
- Pass-cli references used instead of plaintext env vars
- Go adapter zeros credential buffers on terminate (`ZeroCredentials()`)

**Tests**: `tests/integration/credential_security_test.go`

### 3. Path Traversal

**Risk**: Malicious paths like `../../etc/passwd` could read/write arbitrary files.

**Mitigations**:

- **Two layers of validation** (defense-in-depth):
  1. **Go adapter** (`main.go`): `validateFilePath()` rejects paths with `..` segments or null bytes
  2. **Subprocess** (`bridge.ts`): `validateOid()` and `validateLocalPath()` before file operations
- OID validated against `/^[a-f0-9]{64}$/i` — only hex characters reach path construction
- Download output paths validated before use

### 4. Subprocess Resource Exhaustion (DoS)

**Risk**: Unlimited subprocess spawns consuming all system resources.

**Mitigations**:

- Non-blocking channel-based semaphore: maximum 10 concurrent operations
- Immediate error return when semaphore is full (no queuing)
- Per-operation timeout: 5 minutes (configurable via `exec.CommandContext`)
- Process killed on timeout

**Tests**: `cmd/adapter/bridge_test.go` (TestBridgeClientSemaphoreExhaustion)

### 5. Session Token Theft

**Risk**: Session file readable by other users on shared systems.

**Mitigations**:

- Session file at `~/.proton-drive-cli/session.json` contains only revocable tokens (no passwords)
- Directory permissions: `0700` (set by `ensureDir`)
- File permissions: `0600` (set by atomic write)
- Session stored in user home directory, not shared locations
- Tokens can be revoked server-side via `proton-drive logout`

**Validation**: `TestCredentialSessionFilePermissions` in integration tests

### 6. Network Interception

**Risk**: Man-in-the-middle attack on communications.

**Mitigations**:

- All Proton API calls use HTTPS (TLS)
- SRP authentication — password never sent to server
- E2E encryption — file contents encrypted client-side before upload
- No localhost HTTP service — Go adapter communicates with proton-drive-cli via subprocess stdin/stdout (no network exposure)

### 7. Subprocess Environment Isolation

**Risk**: Sensitive environment variables leaking to subprocess.

**Mitigations**:

- Environment allowlist in `bridge.go` — only approved prefixes/names forwarded
- Allowed: PATH, HOME, USER, SHELL, LANG, LC_*, NODE_*, XDG_*, MOCK_BRIDGE_*, PROTON_*, LFS_*, SDK_*, TMPDIR
- Credential environment variables (if set) are only forwarded if they match the allowlist
- Test coverage verifies allowlist behavior

**Tests**: `cmd/adapter/bridge_test.go` (TestFilteredEnv, TestMatchesAllowlist)

### 8. Git Credential Manager Integration

**Risk**: Credentials resolved via `git credential fill` could be intercepted or the git credential helper could be compromised.

**Mitigations**:

- `execFile` used (not `exec`) — prevents shell injection in the git binary path
- 10-second timeout prevents hanging on interactive credential helpers
- Credentials flow: git credential helper -> proton-drive-cli (memory only) -> Proton API
- `git credential approve/reject` used for proper credential lifecycle management
- When `credentialProvider=git-credential` is used, credentials are resolved entirely within proton-drive-cli — they never pass through the Go adapter
- The Go adapter skips pass-cli resolution entirely in git-credential mode, eliminating that attack surface

**Note**: The security of stored credentials depends on the underlying credential helper (macOS Keychain, Windows Credential Manager, etc.). The git credential protocol itself does not encrypt data in transit between git and the helper.

## Known Gaps

1. Debug logging (`--debug`) could expose API response bodies containing tokens. Debug mode should only be used in development.
2. Git credential helpers vary in security: some may store credentials in plaintext files (e.g., `git-credential-store`). Users should prefer OS-integrated helpers (macOS Keychain, GCM).
