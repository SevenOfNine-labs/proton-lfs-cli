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

- Account credentials are not accepted by Git LFS transfer commands
- Local unlock selectors are passed via stdin JSON to subprocess (not visible in `ps aux`)
- Credential flow: browser-fork login stores a UID-scoped key-password entry via proton-drive-cli; Go transfers send only optional mailbox/data-password selectors (`dataCredentialProvider`, `dataCredentialHost`)
- **No HTTP layer** — credentials never traverse network connections, even localhost
- **Passwords are never persisted to disk** — `saveSession()` strips `mailboxPassword` before writing
- **Passwords are never accepted via transfer CLI flags** — browser login and local unlock storage are handled by proton-drive-cli
- Browser-fork sessions use UID-scoped key-password material for Drive unlocks;
  explicit data-password credentials are separate fallback entries and account
  login passwords are not reused by transfers
- Session file (`~/.proton-drive-cli/session.json`) contains only revocable tokens (sessionId, accessToken, refreshToken)
- Session directory `0700`, session file `0600` (owner-only)
- Error messages sanitized — no credential values in responses or logs
- Usernames are not logged (prevents email leak to log files)
- Account-login credential resolution is absent from the Go adapter and transfer bridge

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

- One bridge subprocess per command; the number of adapter processes, and therefore of concurrent bridge subprocesses, is bounded by git-lfs (`lfs.concurrenttransfers`)
- Per-command timeout: `--bridge-timeout` / `PROTON_LFS_BRIDGE_TIMEOUT` (default 5 minutes) plus one second per 256 KiB for uploads and downloads
- On expiry the subprocess's process group is killed with `SIGKILL`, so helpers it spawned (git credential, pass-cli) die with it; `WaitDelay` (5 s) bounds the wait for its pipes
- proton-drive-cli bounds its own stdin read (30 s), each request of its own API client (30 s), and SDK requests with the per-request timeout the SDK sets

**Tests**: `cmd/adapter/bridge_test.go` (timeout scaling, process handling)

### 5. Session Token Theft

**Risk**: Session file readable by other users on shared systems.

**Mitigations**:

- Session file at `~/.proton-drive-cli/session.json` contains only revocable tokens (no passwords)
- Directory permissions: `0700` (set by `ensureDir`)
- File permissions: `0600` (set by atomic write)
- Session stored in user home directory, not shared locations
- `proton-drive-cli logout` deletes the local session file; it makes no API call, so a token that may have leaked is revoked in the Proton account settings ("log out all other sessions")

**Validation**: `TestCredentialSessionFilePermissions` in integration tests

### 6. Network Interception

**Risk**: Man-in-the-middle attack on communications.

**Mitigations**:

- All Proton API calls use HTTPS (TLS)
- Browser-fork login is the only account-login path; Git LFS transfers reuse the saved session
- E2E encryption — file contents encrypted client-side before upload
- No localhost HTTP service — Go adapter communicates with proton-drive-cli via subprocess stdin/stdout (no network exposure)

### 7. Subprocess Environment Isolation

**Risk**: Sensitive environment variables leaking to subprocess.

**Mitigations**:

- Environment allowlist in `bridge.go` — only approved prefixes/names forwarded
- Allowed: `PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `LC_*`, `TERM`, `NODE_ENV`, `NODE_OPTIONS`, `NODE_PATH`, `NODE_BIN`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR`, `TMPDIR`, `TMP`, `TEMP`, the Windows profile and system variables (`USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, `APPDATA`, `LOCALAPPDATA`, `SYSTEMROOT`, `WINDIR`, `COMSPEC`, `PATHEXT`), `MOCK_BRIDGE_*`, and the variables proton-drive-cli reads: `PROTON_DRIVE_CLI_APP_VERSION`, `PROTON_KEY_PASSWORD_PROVIDER`, `PROTON_KEY_PASSWORD_HOST`, `PROTON_DATA_CREDENTIAL_PROVIDER`, `PROTON_DATA_CREDENTIAL_HOST`, `PROTON_PASS_CLI_BIN`, `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID`
- Secret-shaped variables (`PROTON_PASSWORD`, `PROTON_DATA_PASSWORD`, `PROTON_SECOND_FACTOR_CODE`) are not on the allowlist and are never forwarded; `proton-drive-cli doctor` fails when the last two are set in its own environment
- Test coverage verifies allowlist behavior

**Tests**: `cmd/adapter/bridge_test.go` (TestFilteredEnv, TestMatchesAllowlist)

### 8. Git Credential Manager Integration

**Risk**: Credentials resolved via `git credential fill` could be intercepted or the git credential helper could be compromised.

**Mitigations**:

- `execFile` used (not `exec`) — prevents shell injection in the git binary path
- 10-second timeout prevents hanging on interactive credential helpers
- Browser-fork key-password flow: git credential helper -> proton-drive-cli
  key-password store. Git LFS transfers never request account credentials from
  the helper.
- `git credential approve/reject` used for proper credential lifecycle management
- When `--key-password-provider git-credential` is used, the browser-fork key
  password is stored entirely within proton-drive-cli's key-password flow; it
  never passes through the Go adapter.
- When `dataCredentialProvider=git-credential` is used, the mailbox/data password is resolved from a separate host/key (`proton-data.proton-lfs-cli.local` by default)
- The Go adapter resolves no credentials from any provider; proton-drive-cli does, and only for the key password and the optional data password

**Note**: The security of stored credentials depends on the underlying credential helper (macOS Keychain, Windows Credential Manager, etc.). The git credential protocol itself does not encrypt data in transit between git and the helper.

## Known Gaps

1. Debug logging (`--debug`) could expose API response bodies containing tokens. Debug mode should only be used in development.
2. Git credential helpers vary in security: some may store credentials in plaintext files (e.g., `git-credential-store`). Users should prefer OS-integrated helpers (macOS Keychain, GCM).
