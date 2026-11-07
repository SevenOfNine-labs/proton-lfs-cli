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
- Bridge requests (storage base, app version, OIDs and paths) travel as stdin JSON, never as command-line arguments; they carry no credentials
- Subprocess environment is filtered via allowlist (`internal/bridgeproto/env.go`): only named variables such as `PATH`, `HOME`, `NODE_BIN` and the credential-tool settings are forwarded

**Tests**: `cmd/adapter/bridge_test.go` (filteredEnv, matchesAllowlist tests)

### 2. Credential Exposure

**Risk**: Credentials visible in process list, logs, or on disk.

**Mitigations**:

- The account password is entered only on Proton's browser sign-in page
  (`proton-drive-cli login`, browser session fork); no program in this
  repository or in proton-drive-cli accepts or stores it
- The fork delivers the key password for every account type;
  proton-drive-cli stores it with the access and refresh tokens as one
  entry, keyed by the session UID, in the configured credential store
  (`pass-cli` or `git credential`) under `proton-drive-cli.invalid`, reads
  the entry back to verify it after every write, and removes it when login
  cannot save the session (RFC 6749, 10.4: refresh tokens confidential in
  storage; CWE-312)
- Session entry lifecycle: every token refresh rewrites the entry under the
  session lock; `proton-drive-cli logout` removes it after deleting
  session.json; a login that yields a
  new session UID removes the entry of the previous UID; a refresh token
  the API rejects signs the session out and removes it
- The Go adapter resolves no credential and sends none: bridge requests carry
  only `storageBase`, `appVersion` and the command fields (OIDs and paths)
- **No HTTP layer** — the adapter talks to proton-drive-cli over subprocess
  pipes, never over a network connection, even localhost
- Session file (`~/.proton-drive-cli/session.json`) holds no secret: `uid`,
  `scopes`, `credentialStore`, `credentialHost`, `revision` and
  `tokenExpiresAt`
- Session directory `0700`, session file `0600` (owner-only)
- Error messages sanitized: stderr quoted in adapter errors and the tray log
  are redacted by `internal/bridgeproto` (bearer tokens, secret-named fields,
  URL userinfo) before they are cut to length

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

**Risk**: Session tokens read from disk by other users, backups or synchronized home directories.

**Mitigations**:

- The access and refresh tokens live in the credential store entry, never in a file of this project; `~/.proton-drive-cli/session.json` holds no secret
- Directory permissions: `0700` (set by `ensureDir`)
- File permissions: `0600` (set by atomic write, `write-file-atomic`)
- Session stored in user home directory, not shared locations
- `proton-drive-cli logout` removes the session entry, revokes the session on the server (`DELETE /auth/v4`; a failed revocation is logged and the local cleanup still completes) and deletes the session file and crypto cache
- A session revoked elsewhere (Proton account settings, "log out all other sessions") fails at its next API call; a refresh token the API rejects with a Proton error body and HTTP 4xx other than 408 and 429 signs the local session out

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

- Environment allowlist in `internal/bridgeproto/env.go` — only approved prefixes/names forwarded
- Allowed: `PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `LC_*`, `TERM`, `NODE_ENV`, `NODE_OPTIONS`, `NODE_PATH`, `NODE_BIN`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR`, `TMPDIR`, `TMP`, `TEMP`, the Windows profile and system variables (`USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, `APPDATA`, `LOCALAPPDATA`, `SYSTEMROOT`, `WINDIR`, `COMSPEC`, `PATHEXT`), `MOCK_BRIDGE_*`, the variables bridge commands read: `PROTON_DRIVE_CLI_APP_VERSION`, `PROTON_PASS_CLI_BIN`, `PROTON_AUTH_TRACE`, `PROTON_AUTH_TRACE_ID`, and the configuration of the processes they start: `PROTON_PASS_SESSION_DIR`, `PROTON_PASS_KEY_PROVIDER`, `PROTON_PASS_ENCRYPTION_KEY`, `PROTON_PASS_LINUX_KEYRING`, `DBUS_SESSION_BUS_ADDRESS`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`, `GCM_*`, and for Git Credential Manager's `gpg` store `DISPLAY`, `WAYLAND_DISPLAY`, `GPG_TTY`, `SSH_TTY`, `PASSWORD_STORE_DIR`, `GNUPGHOME`, and `WSL_INTEROP`, `WSLENV` for a Windows `git-credential-manager.exe` started from WSL
- Every other variable, secret-shaped names such as `PROTON_PASSWORD` included, is dropped; no proton-drive-cli command reads a password from the environment. `PROTON_PASS_ENCRYPTION_KEY` is forwarded only because pass-cli's `env` key provider reads its local-key secret from it; the user who sets it already exposes it to every process of that shell
- Test coverage verifies allowlist behavior

**Tests**: `cmd/adapter/bridge_test.go` (TestFilteredEnv, TestMatchesAllowlist)

### 8. Git Credential Helper Integration

**Risk**: Session secrets passed to `git credential approve` could reach a helper that writes them in plaintext (`store`) or keeps them only in memory (`cache`), or the git credential helper could be compromised.

**Mitigations**:

- `execFile` used (not `exec`) — prevents shell injection in the git binary path
- Attributes travel on stdin, never as command-line arguments; values containing CR, LF or NUL are rejected
- 10-second timeout on every call; `GIT_TERMINAL_PROMPT=0` with empty `GIT_ASKPASS`/`SSH_ASKPASS`, `credential.interactive=false`, `GCM_INTERACTIVE=false` and `GCM_GUI_PROMPT=false`, so a missing entry fails instead of prompting for credentials; the operating system may still ask to unlock a locked keychain or keyring
- Only a helper that encrypts secrets at rest is used: the first `osxkeychain`, `wincred`, `libsecret`, or `manager` of the system and global git configuration whose Git Credential Manager store encrypts (`keychain`, `wincredman`, `dpapi`, `secretservice`, `gpg`, pinned through `GCM_CREDENTIAL_STORE`); it is pinned with `-c credential.helper= -c credential.helper=<helper>`, because `git credential approve` sends the credential to every configured helper (git-credential(1)); values with quotes, expansions or shell operators are never selected; without such a helper the store fails
- git runs from the file-system root without `GIT_DIR`, so repository configuration never applies; a save runs `reject` before `approve`, so a helper that keeps an existing item cannot leave the previous refresh token in place
- `GCM_PROVIDER=generic` and the reserved host `proton-drive-cli.invalid` (RFC 6761, 6.4) keep Git Credential Manager from probing a server
- `git credential fill/approve/reject` manage the session entry's lifecycle (read, store, remove); every write is read back
- The session secrets never pass through the Go adapter; the Go adapter resolves no credential

**Note**: The git credential protocol does not encrypt data in transit between git and the helper; it runs over pipes between processes of the same user.

## Known Gaps

1. Debug logging (`--debug`) could expose API response bodies containing tokens. Debug mode should only be used in development.
2. The session entry has no per-application access control in Proton Pass, the Linux Secret Service (Secret Service API, 10) or the Windows Credential Manager: any process of the same user can read it while the store is unlocked.
