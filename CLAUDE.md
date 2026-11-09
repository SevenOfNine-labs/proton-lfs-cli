# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Proton LFS CLI - A pre-alpha Git LFS custom transfer adapter for Proton Drive that provides encrypted storage for Git LFS objects.

**Current State:** The Go adapter communicates directly with `proton-drive-cli` via subprocess stdin/stdout. A system tray app provides GUI status and configuration. The release pipeline bundles all three components (adapter + tray + proton-drive-cli SEA) into a single distributable per platform.

## Architecture

```
System Tray App (cmd/tray/)
     ↕  reads ~/.proton-lfs/status.json
     ↕  writes ~/.proton-lfs/config.json
Go Adapter (cmd/adapter/) → proton-drive-cli (subprocess, stdin/stdout JSON) → Proton API
                                    ↓
                        pass-cli or git-credential
       (session tokens and key password, stored and read by proton-drive-cli)
```

Shared configuration lives in `internal/config/` and is used by both the adapter and tray app.

The project has five main components:

1. **Go Adapter** (`cmd/adapter/`): Custom transfer adapter that implements Git LFS protocol
   - `main.go`: Core adapter logic, message handling, protocol implementation, status reporting
   - `backend.go`: Storage backend abstraction (local and DriveCLI backends)
   - `bridge.go`: Direct subprocess client for proton-drive-cli (stdin/stdout JSON protocol)
   - `config_constants.go`: Thin wrapper delegating to `internal/config`

2. **System Tray App** (`cmd/tray/`): Cross-platform menu bar application
   - `main.go`: Entry point, subcommand dispatch, `--version`, single-instance lock, login-shell PATH adoption (macOS and Linux)
   - `menu.go`: Menu structure, credential store toggle, LFS registration, Open Logs, Run Doctor, native notifications
   - `connect.go`: "Connect to Proton" browser-fork login flow and login rate-limit guard
   - `cli.go`: CLI subcommand handlers (login, logout, status, config, register)
   - `scope_diagnostics.go`: `scope-diagnostics [--live]` redacted auth/scope report
   - `status.go`: Polls status.json every 5s, derives readiness from session.json, schedules `session refresh --json`, updates icon/tooltip/checkmarks
   - `setup.go`: Binary discovery, autostart (macOS LaunchAgent `com.proton.lfs-tray` / Linux .desktop)
   - `icons/`: Embedded 64x64 PNG icons (idle/ok/error/syncing)

3. **Shared Config** (`internal/config/`): Constants and helpers shared by adapter + tray
   - `config.go`: Env var names, defaults, `EnvTrim`/`EnvOrDefault`/`EnvBoolOrDefault`
   - `status.go`: `StatusReport` struct, atomic `WriteStatus`/`ReadStatus`
   - `prefs.go`: `Preferences` struct (`credentialStore`), `LoadPrefs`/`SavePrefs`
   - `fsutil.go`: `WriteFileAtomic` (temporary file, fsync, rename)
   - `internal/bridgeproto/`: bridge contract shared by adapter and tray: response envelope parser, environment allowlist, redaction

4. **Preflight** (`internal/preflight/`): Parses `proton-drive-cli doctor --json` against the bridge auth-state schema; used by `scripts/check_doctor_readiness.go` and the real E2E test before any live canary

5. **Submodules** (`submodules/`):
   - `git-lfs`: Upstream Git LFS reference
   - `pass-cli`: Proton Pass CLI for secure credential storage (2.7.0)
   - `proton-drive-cli`: TypeScript-based Proton Drive client with full auth flow, E2E encryption

## Development Commands

### Setup and Build
```bash
make setup              # Install deps (Go + JS), create .env
make build              # Build Go adapter to bin/git-lfs-proton-adapter
make build-tray         # Build system tray app (cgo only on macOS)
make build-sea          # Build proton-drive-cli as standalone SEA binary (Node.js per .node-version)
make build-bundle       # Build all 3 components into dist/ for packaging
make build-all          # Build adapter + tray + Git LFS submodule + proton-drive-cli
make build-drive-cli    # Build proton-drive-cli TypeScript bridge only
make install            # Build bundle + install (.app on macOS, ~/.local/bin on Linux)
make uninstall          # Remove installed bundle
```

### Testing
```bash
make test               # Run Go unit tests for ./cmd/... and ./internal/...
make test-integration   # Run Git LFS integration tests
make test-integration-sdk  # SDK backend integration (uses pass-cli)
make test-integration-failure-modes     # Failure mode tests (wrong OID, crash, hang)
make test-integration-config-matrix    # Direction config matrix tests
make test-integration-credentials       # Credential security tests
make test-e2e-mock      # Mocked E2E pipeline (no real credentials)
make test-e2e-real      # Real Proton Drive E2E (guarded: exact PROTON_LFS_LIVE_CANARY acknowledgement + LIVE_CANARY_DOCTOR_ARGS; runs live-drive-scope-canary first)
```

### Linting and Formatting
```bash
make fmt                # Format Go code
make lint               # Run go vet, golangci-lint and the comment policy check
```

### Credential Management
```bash
pass-cli login          # Authenticate with Proton Pass
eval "$(make -s pass-env)"  # Export the pass-cli binary path only
./scripts/export-pass-env.sh  # Direct pass-cli env export
```

## Backend Modes

The adapter supports two backends controlled by `PROTON_LFS_BACKEND`:

1. **local** (default): Local filesystem storage for testing
   - Stores objects in `PROTON_LFS_LOCAL_STORE_DIR`
   - No authentication required
   - Used for protocol integration tests

2. **sdk**: Proton Drive integration via proton-drive-cli subprocess
   - Go adapter spawns `proton-drive-cli bridge <command>` directly (under `node` for a `.js` entry point)
   - Requires an existing browser-fork Proton Drive session
   - Configurable via `--drive-cli-bin` flag or `PROTON_DRIVE_CLI_BIN` env var

## Browser-Fork Auth

Account login is browser-fork-only and happens in `proton-drive-cli login`
before Git LFS transfers; two-factor authentication and CAPTCHA are
completed in the browser. The fork delivers the key password for every
account type; there is no separate mailbox/data password. proton-drive-cli
stores the session secrets (access token, refresh token and key password)
as one entry, keyed by the session UID, in the chosen credential store, and
writes only non-secret fields to `~/.proton-drive-cli/session.json`: UID,
scopes, credential store and host, token expiry and a revision that changes
with every login and refresh. Bridge commands, `doctor` and `logout` use the
recorded store. The Go adapter never sends account username/password
fields, credential selectors, `credentialProvider`, or `allowLogin`: bridge
requests carry only `appVersion`, `storageBase` and the command fields. The
tray's credential store preference is `credentialStore` in
`~/.proton-lfs/config.json`, passed to `login` as `--credential-store`.

Session entry lifecycle: every token refresh rewrites it under the session
lock; `proton-drive-cli logout` revokes the session, deletes session.json
and then removes it; a login that yields a new UID removes the previous
UID's entry; a refresh token the API rejects with a Proton error body and
HTTP 4xx other than 408 and 429, when no other process rotated it, signs
the session out (session.json, crypto cache and the entry deleted,
`auth-state` then reports `needs_login`). An entry the store cannot return
makes `auth-state` report `credentials_unavailable`.

### pass-cli (default)

`proton-drive-cli` can store the session entry in Proton Pass.

```bash
PROTON_CREDENTIAL_STORE=pass-cli  # Read by proton-drive-cli login and the Makefile's live targets
PROTON_PASS_CLI_BIN=pass-cli      # Binary path (respected by proton-drive-cli)
```

**Setup:**
```bash
pass-cli login
proton-drive-cli login --credential-store pass-cli
```

### git-credential

Stores the session entry through git's credential helper protocol
(git-credential(1)). proton-drive-cli uses only the first helper of the
system and global git configuration that encrypts secrets at rest
(`osxkeychain`, `wincred`, `libsecret`, or `manager` with an encrypting
Git Credential Manager store), from `credential.helper` or the host-scoped
`credential.https://proton-drive-cli.invalid.helper`, and pins it, so
`store` or `cache` never receive the session secrets:

```bash
# WSL2: Windows Git Credential Manager for this host only
git config --global 'credential.https://proton-drive-cli.invalid.helper' \
  '/mnt/c/Program\ Files/Git/mingw64/bin/git-credential-manager.exe'
PROTON_CREDENTIAL_STORE=git-credential
proton-drive-cli login --credential-store git-credential
```

## proton-drive-cli Bridge

The `proton-drive-cli` submodule (`submodules/proton-drive-cli/`) provides:
- Browser-fork authentication and session management (`src/auth/`)
- Session management with token refresh
- Drive operations through the official SDK (`src/sdk/`)
- OpenPGP crypto operations (`src/crypto/`)
- Bridge command for Git LFS integration (`src/cli/bridge.ts`)

**Bridge protocol**: `proton-drive-cli bridge <command>` reads JSON from stdin, writes JSON to stdout using the `{ ok, payload, error, code, details }` envelope (schemas under `schemas/bridge/v1/`).

**OID path mapping**: `oidToPath` in `src/bridge/validators.ts` uses 2-level prefix directories (e.g., OID `abc12345...` → `/LFS/ab/c1/abc12345...`); `src/cli/bridge-helpers.ts` wraps it as `oidFileLocation`.

## Important Configuration Files

- `.env` / `.env.example`: Environment configuration (backend, paths, timeouts, credential store defaults; never credentials)
- `Makefile`: Build orchestration, test runners, prerequisite checks
- `submodules/proton-drive-cli/package.json` and `yarn.lock`: the JavaScript dependencies (the root has no JS project)
- `go.mod`: Go 1.25 module (deps: `fyne.io/systray`, `github.com/gofrs/flock`)
- `docs/reference/adapter.md`: every adapter flag and environment variable

## JavaScript Package Management

proton-drive-cli owns its JavaScript dependencies: `package.json` and
`yarn.lock` live in `submodules/proton-drive-cli/`, which Yarn treats as its
own project. Yarn 4 comes from Corepack through `packageManager`; npm cannot
install the `portal:` dependency on the vendored SDK.
```bash
corepack enable
make install-deps            # go mod download + yarn install in the submodule
cd submodules/proton-drive-cli && yarn build && yarn test
```

## Testing Strategy

1. **Unit tests**: Go (`*_test.go`) with `TestHelperProcess` pattern for subprocess mocking
   - `cmd/adapter/`: Protocol, backend, bridge and bridge contract tests (request fields, auth states, error codes against `submodules/proton-drive-cli/schemas/bridge/v1/`)
   - `cmd/tray/`: CLI, readiness, refresh, registration, scope-diagnostics, autostart tests
   - `internal/config/`: Status round-trip, prefs round-trip, env helper tests
   - `internal/preflight/`: doctor readiness parser tests
2. **Integration tests**: `tests/integration/` with `-tags integration`
   - Black-box Git LFS protocol validation
   - Timeout and concurrency stress tests
   - SDK backend roundtrip tests (direct subprocess)
   - Credential security tests
3. **Mock mode**: `ADAPTER_ALLOW_MOCK_TRANSFERS=true` for protocol-only testing (off by default; mock transfers fail closed)
4. **Pre-commit hooks**: `gofmt`, `go vet`, `golangci-lint`, `go test ./cmd/... ./internal/...`, comment policy, doc links, Conventional Commits

## Security Notes

- Never commit credentials to `.env` (use `.env.example` patterns)
- Account login is browser-fork-only and must happen outside Git LFS transfer commands
- Transfer bridge requests never contain account username/password fields, credential selectors, `credentialProvider`, or `allowLogin`; they travel via stdin (never command-line args)
- Environment allowlist (`internal/bridgeproto/env.go`) filters subprocess env (named variables such as PATH, HOME, NODE_BIN, MOCK_BRIDGE_* and the credential-tool settings; `PROTON_CREDENTIAL_STORE` is not forwarded)
- OID validation: `/^[a-f0-9]{64}$/i` before subprocess spawn
- Path traversal prevention: reject paths containing `..`
- One bridge subprocess per command, bounded by `--bridge-timeout` (default 5 min, plus 1 s per 256 KiB for transfers) and killed with its process group on expiry; concurrency comes from git-lfs starting one adapter process per transfer slot
- Session tokens and key password live in the credential store; `~/.proton-drive-cli/session.json` (0600, directory 0700) holds no secret
- See `docs/security/threat-model.md` for full threat model

## Status File Protocol

The adapter writes status to `~/.proton-lfs/status.json` (override with `PROTON_LFS_STATUS_FILE`). The tray app polls this file every 5 seconds.

```json
{ "state": "ok", "lastOid": "abc123...", "lastOp": "upload", "timestamp": "..." }
```

Fields: `state`, `lastOid`, `lastOp`, `error`, `errorCode`, `errorDetail`, `retryable`, `temporary`, `timestamp` (`internal/config/status.go`). States: `ok`, `transferring`, `error`, `rate_limited`, `auth_required`, `captcha`; the adapter writes only transfer results (never `transferring`, nothing on `init` or `terminate`). The tray has four icons (idle grey, ok green, error red, syncing blue); `rate_limited`, `auth_required` and `captcha` share the error icon and differ by tooltip. Reference: `docs/reference/status-file.md`.

## Release Pipeline

`release-bundle.yml` triggers on `v*` tags and produces self-contained bundles:

1. **adapters**: calls `build.yml` (`workflow_call`) for the standalone adapter binaries
2. **build-all** (matrix: macos-14, ubuntu, windows): Builds adapter and tray via `scripts/build-go.sh` (cgo only for the macOS tray), proton-drive-cli SEA via `scripts/build-sea.sh` (Node.js per `.node-version`, `--build-sea`)
3. **package**: Assembles reproducible platform bundles (macOS ad-hoc signed `.app` zip, Linux `.tar.gz`, Windows `.zip`) via `scripts/package-bundle.sh`
4. **release**: Creates the single GitHub Release with every asset and one `SHA256SUMS.txt`

`build.yml` on its own (pushes, PRs) only uploads adapter artifacts; it never creates a release. `proton-drive-cli` is not published to npm.

## Known Issues

1. CAPTCHA may require manual intervention for new Proton accounts (completed in the browser during `proton-drive-cli login`)
2. The sdk backend reports progress once per object after the bridge command returns; interrupted transfers are not resumed

## Code Comments and Legacy (MANDATORY)

Comments carry only what the code cannot say: the normative source behind a
decision. Every comment is one of:

- a reference to a standard or specification, with the section where useful
  (RFC 9580 OpenPGP, RFC 9110 HTTP, RFC 2119 keywords, POSIX `rename(2)`,
  Git LFS custom transfer specification, Desktop Entry Specification 1.5,
  XDG Base Directory, JSON Schema 2020-12, NIST SP 800-38D AES-GCM, ...);
- a reference to a scientific publication or a named algorithm (SRP-6a,
  Argon2, ...);
- a reference to external documentation of an API or tool the code relies
  on, naming the file, function or URL (Proton Drive SDK and account module
  sources, Node.js, Go standard library, man pages);
- a one- or two-line statement of an invariant or contract that a reader
  cannot infer from the code (for example "rename(2) replaces the target
  atomically"), or the API contract in a doc comment of an exported symbol.

Forbidden in comments, documentation and commit-facing text alike:

- history and legacy notes: what the code did before, earlier versions,
  former names, "previously", "no longer", "used to", "kept for
  compatibility", "since version X", migration stories;
- excuses, hedges and placeholders: "for now", "workaround", "hack",
  "temporary fix", "TODO", "FIXME", "XXX", "unfortunately", "hopefully";
- narration of what the next lines do, change logs, author notes, and
  references to issues or pull requests in place of the specification;
- comments longer than three lines outside doc comments of exported APIs.

No legacy code: code, options, configuration keys, file formats, schema
fields and documentation that exist only to serve an earlier version are
deleted, not kept behind a comment or a migration. History lives in Git and
in the generated changelog.

`scripts/check-comment-policy.sh` enforces the forbidden vocabulary on every
commit (pre-commit) and in CI; reviews treat any other violation of this
section as a defect.

## Changeset Tracking (MANDATORY)

Every code change **must** be accompanied by updates to two files in the `.changeset/` directory (git-ignored, never committed):

1. **`.changeset/PR_SUMMARY.md`** — A detailed, always-current summary of all changes in the working branch. Update this after every modification. Include:
   - What changed and why
   - Files added/modified/deleted
   - Testing evidence or instructions
   - Any breaking changes or migration notes

2. **`.changeset/COMMIT_MESSAGE.md`** — A ready-to-use commit message following [Conventional Commits](https://www.conventionalcommits.org/). Update this after every modification. Format:
   ```
   <type>(<scope>): <subject>          ← max 72 chars total

   - bullet point details of changes   ← wrap at 72 chars
   - one bullet per logical change
   ```
   Valid types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`, `perf`, `build`.

**Workflow**: Create `.changeset/` dir on first change if it doesn't exist. Update both files after every file edit, creation, or deletion — before moving to the next task.

## Development Workflow

1. Make changes to Go adapter
2. Run unit tests: `make test`
3. Run integration tests: `make test-integration`
4. For SDK path: Ensure pass-cli login, then `make test-integration-sdk`
5. Run linting: `make lint`
6. Format code: `make fmt`
7. Check docs match runtime behavior (canonical rule: tests > docs)

## References

- Git LFS custom transfer spec: `submodules/git-lfs/docs/custom-transfers.md`
- Pass CLI docs: `submodules/pass-cli/docs/`
- proton-drive-cli: `submodules/proton-drive-cli/`
- Project docs: Start at `docs/README.md`
