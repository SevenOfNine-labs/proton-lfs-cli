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
                    (credentials resolved internally)
```

Shared configuration lives in `internal/config/` and is used by both the adapter and tray app.

The project has five main components:

1. **Go Adapter** (`cmd/adapter/`): Custom transfer adapter that implements Git LFS protocol
   - `main.go`: Core adapter logic, message handling, protocol implementation, status reporting
   - `backend.go`: Storage backend abstraction (local and DriveCLI backends)
   - `bridge.go`: Direct subprocess client for proton-drive-cli (stdin/stdout JSON protocol)
   - `config_constants.go`: Thin wrapper delegating to `internal/config`

2. **System Tray App** (`cmd/tray/`): Cross-platform menu bar application
   - `main.go`: Entry point, `--version` flag, PATH augmentation for macOS
   - `menu.go`: Menu structure, key-password provider toggle, LFS registration, native notifications
   - `connect.go`: "Connect to Proton" browser-fork login flow
   - `cli.go`: CLI subcommand handlers (login, logout, status, config, register)
   - `status.go`: Polls status.json every 5s, updates icon/tooltip/checkmarks
   - `setup.go`: Binary discovery, autostart (macOS LaunchAgent / Linux .desktop)
   - `icons/`: Embedded 64x64 PNG icons (idle/ok/error/syncing)

3. **Shared Config** (`internal/config/`): Constants and helpers shared by adapter + tray
   - `config.go`: Env var names, defaults, `EnvTrim`/`EnvOrDefault`/`EnvBoolOrDefault`
   - `status.go`: `StatusReport` struct, atomic `WriteStatus`/`ReadStatus`
   - `prefs.go`: `Preferences` struct, `LoadPrefs`/`SavePrefs`

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
make lint               # Run Go vet + golangci-lint
```

### Credential Management
```bash
pass-cli login          # Authenticate with Proton Pass
eval "$(make -s pass-env)"  # Export pass-cli provider binary only
./scripts/export-pass-env.sh  # Direct provider-env export
```

## Backend Modes

The adapter supports two backends controlled by `PROTON_LFS_BACKEND`:

1. **local** (default): Local filesystem storage for testing
   - Stores objects in `PROTON_LFS_LOCAL_STORE_DIR`
   - No authentication required
   - Used for protocol integration tests

2. **sdk**: Proton Drive integration via proton-drive-cli subprocess
   - Go adapter spawns `node proton-drive-cli bridge <command>` directly
   - Requires an existing browser-fork Proton Drive session
   - Configurable via `--drive-cli-bin` flag or `PROTON_DRIVE_CLI_BIN` env var

## Browser-Fork Auth

Account login is browser-fork-only and happens in `proton-drive-cli login`
before Git LFS transfers. The Go adapter never sends account username/password
fields, `credentialProvider`, or `allowLogin` to bridge transfer commands.
The key-password provider preference is `keyPasswordProvider` in
`~/.proton-lfs/config.json`; `--credential-provider` and
`PROTON_CREDENTIAL_PROVIDER` do not exist.

### pass-cli (default)

`proton-drive-cli` can store the browser-fork key password in Proton Pass.

```bash
PROTON_KEY_PASSWORD_PROVIDER=pass-cli
PROTON_PASS_CLI_BIN=pass-cli           # Binary path (respected by proton-drive-cli)
```

**Setup:**
```bash
pass-cli login
proton-drive-cli login --key-password-provider pass-cli
```

### git-credential

Uses Git Credential Manager (GCM), macOS Keychain, Windows Credential Manager,
or Linux Secret Service to store the browser-fork key password:

```bash
PROTON_KEY_PASSWORD_PROVIDER=git-credential
proton-drive-cli login --key-password-provider git-credential
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

- `.env` / `.env.example`: Environment configuration (credentials, backend modes)
- `Makefile`: Build orchestration, test runners, prerequisite checks
- `package.json`: Root Yarn 4 workspace with `proton-drive-cli`
- `go.mod`: Go 1.25 module (deps: `fyne.io/systray`)
- `docs/reference/adapter.md`: every adapter flag and environment variable

## JavaScript Package Management

Uses **Yarn 4** (Berry) via Corepack:
```bash
corepack enable
corepack prepare yarn@4.1.1 --activate
yarn install

# Fallback to npm if Yarn unavailable
make setup JS_PM=npm
```

Workspace structure:
```json
{
  "workspaces": ["submodules/proton-drive-cli"]
}
```

## Testing Strategy

1. **Unit tests**: Go (`*_test.go`) with `TestHelperProcess` pattern for subprocess mocking
   - `cmd/adapter/`: Protocol, backend, bridge, credential tests
   - `internal/config/`: Status round-trip, prefs round-trip, env helper tests
2. **Integration tests**: `tests/integration/` with `-tags integration`
   - Black-box Git LFS protocol validation
   - Timeout and concurrency stress tests
   - SDK backend roundtrip tests (direct subprocess)
   - Credential security tests
3. **Mock mode**: `ADAPTER_ALLOW_MOCK_TRANSFERS=true` for protocol-only testing
4. **Pre-commit hooks**: `gofmt`, `go vet`, `golangci-lint`, `go test` (adapter + config)

## Security Notes

- Never commit credentials to `.env` (use `.env.example` patterns)
- Account login is browser-fork-only and must happen outside Git LFS transfer commands
- Transfer bridge requests never contain account username/password fields, `credentialProvider`, or `allowLogin`
- Optional mailbox/data-password selectors are passed via stdin to subprocesses (never command-line args)
- Environment allowlist filters subprocess env (only PATH, HOME, NODE_*, MOCK_BRIDGE_*, etc.)
- OID validation: `/^[a-f0-9]{64}$/i` before subprocess spawn
- Path traversal prevention: reject paths containing `..`
- One bridge subprocess per command, bounded by `--bridge-timeout` (default 5 min, plus 1 s per 256 KiB for transfers) and killed with its process group on expiry; concurrency comes from git-lfs starting one adapter process per transfer slot
- Session tokens stored in `~/.proton-drive-cli/session.json` (should be 0600)
- See `docs/security/threat-model.md` for full threat model

## Status File Protocol

The adapter writes status to `~/.proton-lfs/status.json` (override with `PROTON_LFS_STATUS_FILE`). The tray app polls this file every 5 seconds.

```json
{ "state": "ok", "lastOid": "abc123...", "lastOp": "upload", "timestamp": "..." }
```

States: `idle`, `ok`, `transferring`, `error`, `rate_limited`, `auth_required`, `captcha`. The tray has four icons (idle grey, ok green, error red, syncing blue); `rate_limited`, `auth_required` and `captcha` share the error icon and differ by tooltip.

## Release Pipeline

`release-bundle.yml` triggers on `v*` tags and produces self-contained bundles:

1. **adapters**: calls `build.yml` (`workflow_call`) for the standalone adapter binaries
2. **build-all** (matrix: macos-14, ubuntu, windows): Builds adapter and tray via `scripts/build-go.sh` (cgo only for the macOS tray), proton-drive-cli SEA via `scripts/build-sea.sh` (Node.js per `.node-version`, `--build-sea`)
3. **package**: Assembles reproducible platform bundles (macOS ad-hoc signed `.app` zip, Linux `.tar.gz`, Windows `.zip`) via `scripts/package-bundle.sh`
4. **release**: Creates the single GitHub Release with every asset and one `SHA256SUMS.txt`

`build.yml` on its own (pushes, PRs) only uploads adapter artifacts; it never creates a release. `proton-drive-cli` is not published to npm.

## Known Issues

1. `Mock transfers are fail-closed by default` (require explicit opt-in)
2. CAPTCHA may require manual intervention for new Proton accounts

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
