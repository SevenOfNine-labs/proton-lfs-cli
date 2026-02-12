# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Proton Git LFS Backend - A pre-alpha Git LFS custom transfer adapter for Proton Drive that provides encrypted storage for Git LFS objects.

**Current State:** The Go adapter communicates directly with `proton-drive-cli` via subprocess stdin/stdout. A system tray app provides GUI status and configuration. The release pipeline bundles all three components (adapter + tray + proton-drive-cli SEA) into a single distributable per platform.

## Architecture

```
System Tray App (cmd/tray/)
     ↕  reads ~/.proton-git-lfs/status.json
     ↕  writes ~/.proton-git-lfs/config.json
Go Adapter (cmd/adapter/) → proton-drive-cli (subprocess, stdin/stdout JSON) → Proton API
     ↓
 pass-cli or git-credential (credentials)
```

Shared configuration lives in `internal/config/` and is used by both the adapter and tray app.

The project has four main components:

1. **Go Adapter** (`cmd/adapter/`): Custom transfer adapter that implements Git LFS protocol
   - `main.go`: Core adapter logic, message handling, protocol implementation, status reporting
   - `backend.go`: Storage backend abstraction (local and DriveCLI backends)
   - `bridge.go`: Direct subprocess client for proton-drive-cli (stdin/stdout JSON protocol)
   - `passcli.go`: Credential resolution via pass-cli integration
   - `config_constants.go`: Thin wrapper delegating to `internal/config`

2. **System Tray App** (`cmd/tray/`): Cross-platform menu bar application
   - `main.go`: Entry point using `fyne.io/systray`
   - `menu.go`: Menu structure, credential provider toggle, Git LFS registration
   - `status.go`: Polls `~/.proton-git-lfs/status.json` every 5s, updates icon/text
   - `setup.go`: Binary discovery, autostart (macOS LaunchAgent / Linux .desktop)
   - `icons/`: Embedded 64x64 PNG icons (idle/ok/error/syncing)

3. **Shared Config** (`internal/config/`): Constants and helpers shared by adapter + tray
   - `config.go`: Env var names, defaults, `EnvTrim`/`EnvOrDefault`/`EnvBoolOrDefault`
   - `status.go`: `StatusReport` struct, atomic `WriteStatus`/`ReadStatus`
   - `prefs.go`: `Preferences` struct, `LoadPrefs`/`SavePrefs`

4. **Submodules** (`submodules/`):
   - `git-lfs`: Upstream Git LFS reference
   - `pass-cli`: Proton Pass CLI for secure credential storage (v1.4.3)
   - `proton-drive-cli`: TypeScript-based Proton Drive client with full auth flow, E2E encryption

## Development Commands

### Setup and Build
```bash
make setup              # Install deps (Go + JS), create .env
make build              # Build Go adapter to bin/git-lfs-proton-adapter
make build-tray         # Build system tray app (requires CGO_ENABLED=1)
make build-sea          # Build proton-drive-cli as standalone SEA binary (Node.js 25.5+)
make build-bundle       # Build all 3 components into dist/ for packaging
make build-all          # Build adapter + tray + Git LFS submodule + proton-drive-cli
make build-drive-cli    # Build proton-drive-cli TypeScript bridge only
```

### Testing
```bash
make test               # Run Go adapter unit tests
make test-integration   # Run Git LFS integration tests
make test-integration-sdk  # SDK backend integration (uses pass-cli)
make test-integration-failure-modes     # Failure mode tests (wrong OID, crash, hang)
make test-integration-config-matrix    # Direction config matrix tests
make test-integration-credentials       # Credential security tests
make test-e2e-mock      # Mocked E2E pipeline (no real credentials)
make test-e2e-real      # Real Proton Drive E2E (requires pass-cli login + build-drive-cli)
```

### Linting and Formatting
```bash
make fmt                # Format Go code
make lint               # Run Go vet + golangci-lint
```

### Credential Management
```bash
pass-cli login          # Authenticate with Proton Pass
eval "$(make -s pass-env)"  # Export credentials from Pass
./scripts/export-pass-env.sh  # Direct credential export
```

## Backend Modes

The adapter supports two backends controlled by `PROTON_LFS_BACKEND`:

1. **local** (default): Local filesystem storage for testing
   - Stores objects in `PROTON_LFS_LOCAL_STORE_DIR`
   - No authentication required
   - Used for protocol integration tests

2. **sdk**: Proton Drive integration via proton-drive-cli subprocess
   - Go adapter spawns `node proton-drive-cli bridge <command>` directly
   - Requires Proton credentials (resolved via pass-cli or git-credential)
   - Configurable via `--drive-cli-bin` flag or `PROTON_DRIVE_CLI_BIN` env var

## Credential Resolution

The adapter supports two credential providers controlled by `PROTON_CREDENTIAL_PROVIDER`:

### pass-cli (default)

The Go adapter (`cmd/adapter/passcli.go`) resolves credentials via pass-cli:

```bash
# Environment variables
PROTON_CREDENTIAL_PROVIDER=pass-cli    # Default
PROTON_PASS_CLI_BIN=pass-cli           # Binary path
PROTON_PASS_REF_ROOT=pass://Personal/Proton Git LFS
PROTON_PASS_USERNAME_REF=pass://Personal/Proton Git LFS/username
PROTON_PASS_PASSWORD_REF=pass://Personal/Proton Git LFS/password
```

Credentials are resolved via pass-cli. The adapter calls `pass-cli item view <reference>` and parses JSON or plaintext output.

### git-credential

Uses Git Credential Manager (GCM) to resolve credentials from macOS Keychain, Windows Credential Manager, or Linux Secret Service:

```bash
# Environment variable
PROTON_CREDENTIAL_PROVIDER=git-credential

# Or CLI flag
--credential-provider git-credential
```

In this mode, the Go adapter skips pass-cli entirely and sends `{ "credentialProvider": "git-credential" }` to proton-drive-cli via stdin. `proton-drive-cli` then resolves credentials locally via `git credential fill` — credentials never leave the local machine.

**Setup:**
```bash
# Store credentials in the system credential helper
proton-drive credential store -u your.email@proton.me

# Verify credentials are stored
proton-drive credential verify
```

**Standalone CLI commands** also accept `--credential-provider git`:
```bash
proton-drive ls / --credential-provider git
proton-drive upload ./file.pdf /Documents --credential-provider git
```

## proton-drive-cli Bridge

The `proton-drive-cli` submodule (`submodules/proton-drive-cli/`) provides:
- Complete SRP authentication flow (`src/auth/`)
- Session management with token refresh
- File upload/download with E2E encryption (`src/drive/`)
- OpenPGP crypto operations (`src/crypto/`)
- Bridge command for Git LFS integration (`src/cli/bridge.ts`)

**Bridge protocol**: `proton-drive-cli bridge <command>` reads JSON from stdin, writes JSON to stdout using `{ ok, payload, error, code }` envelope format.

**Bridge helpers** (`src/cli/bridge-helpers.ts`): OID-to-path mapping using 2-level prefix directories (e.g., OID `abc12345...` → `/LFS/ab/c1/abc12345...`).

## Important Configuration Files

- `.env` / `.env.example`: Environment configuration (credentials, backend modes)
- `Makefile`: Build orchestration, test runners, prerequisite checks
- `package.json`: Root Yarn 4 workspace with `proton-drive-cli`
- `go.mod`: Go 1.25 module (deps: `fyne.io/systray`)
- `docs/architecture/sdk-capability-matrix.md`: SDK access requirements by mode

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
- Credentials resolve via pass-cli (`pass://...` references) or git-credential (`git credential fill`)
- Credentials flow (pass-cli): pass-cli → Go adapter → proton-drive-cli (via stdin)
- Credentials flow (git-credential): git credential helper → proton-drive-cli (local, never over network)
- Credentials are passed via stdin to subprocesses (never command-line args)
- Environment allowlist filters subprocess env (only PATH, HOME, NODE_*, MOCK_BRIDGE_*, etc.)
- OID validation: `/^[a-f0-9]{64}$/i` before subprocess spawn
- Path traversal prevention: reject paths containing `..`
- Subprocess pool: max 10 concurrent operations with 5-minute timeout (non-blocking semaphore)
- Session tokens stored in `~/.proton-drive-cli/session.json` (should be 0600)
- See `docs/security/threat-model.md` for full threat model

## Status File Protocol

The adapter writes status to `~/.proton-git-lfs/status.json` (override with `PROTON_LFS_STATUS_FILE`). The tray app polls this file every 5 seconds.

```json
{ "state": "ok", "lastOid": "abc123...", "lastOp": "upload", "timestamp": "..." }
```

States: `idle` (grey icon), `ok` (green), `error` (red), `transferring` (blue).

## Release Pipeline

`release-bundle.yml` triggers on `v*` tags and produces self-contained bundles:

1. **build-all** (matrix: macos-14/13, ubuntu, windows): Builds adapter (CGO=0), tray (CGO=1), proton-drive-cli SEA (Node.js 25.5+ `--build-sea`)
2. **package**: Assembles platform bundles (macOS `.app`, Linux `.tar.gz`, Windows `.zip`) via `scripts/package-bundle.sh`
3. **release**: Creates GitHub Release with SHA256 checksums

The existing `build.yml` continues to build standalone adapter binaries.

## Known Issues

1. `proton-drive-cli session refresh not working properly` (noted in its README)
2. `Mock transfers are fail-closed by default` (require explicit opt-in)
3. CAPTCHA may require manual intervention for new Proton accounts

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
