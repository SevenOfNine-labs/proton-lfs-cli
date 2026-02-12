# Proton Git-LFS Workspace Guidelines

## Code Style
- **Go**: Format with `goimports` (enforced by Makefile). Follow standard Go conventions. Use discrete command modules in `commands/` directory.
- **TypeScript** (proton-drive-cli): Check `submodules/proton-drive-cli/` for TypeScript patterns and conventions.

## Architecture
Two-tier integration:
1. **Git LFS** (`submodules/git-lfs/`) - Go-based Git extension managing `.gitattributes` patterns, pointer file handling, and upload/download coordination
2. **Custom Transfer Adapter** (`cmd/adapter/`) - Subprocess handling JSON-RPC protocol between Git LFS and proton-drive-cli (spawned directly as a subprocess with JSON stdin/stdout)

**Design principle**: End-user-first with minimal required configuration. Server-side push during `git push`, client-side download on checkout. Standalone mode (no LFS API server) enabled via `lfs.standalonetransferagent = proton` config.

## Build and Test
From `submodules/git-lfs/`:
- `make` - Build Git LFS
- `make test` - Run Go test suite (append `GO_TEST_EXTRA_ARGS` for custom flags)
- Cross-platform support: builds for Linux, macOS, Windows, FreeBSD

From project root:
- `go build -o bin/git-lfs-proton-adapter ./cmd/adapter/` - Build Proton custom adapter
- `make test` - Test proton-drive-cli bridge

## Project Conventions
- Feature requests/discussions: Post to Discussions channel (non-bug topics)
- PR requirements: Submit to `main` branch with tests, documentation, and focused scope
- Commands: Organize discrete features in `commands/command_*.go` modules
- Custom adapter: Implemented as standalone subprocess in `cmd/adapter/main.go`
- SDK operations: All Proton Drive interactions must route through SDK—no direct API calls
- HTTP headers: SDKs require `x-pm-appversion: external-drive-protonlfs@{version}` header for all requests
- Documentation: See [Documentation Maintenance](#documentation-maintenance) section below

## Custom Transfer Adapter Integration

### Location & Structure
- **Binary entry point**: `cmd/adapter/main.go` - Reads JSON messages from stdin, writes responses to stdout
- **Protocol**: Line-delimited JSON (one message per line with `\n` terminator)
- **Lifecycle**: Started by Git LFS, receives init → transfers → terminate
- **Concurrency**: Git LFS may spawn multiple adapter processes in parallel (controlled by `lfs.concurrenttransfers`)
- **Registration**: Git LFS discovers via `lfs.customtransfer.proton.path` config during initialization

### Key Integration Points
1. **In `submodules/git-lfs/tq/manifest.go`**: `configureCustomAdapters()` reads Git config and registers adapter factory
2. **In `submodules/git-lfs/tq/custom.go`**: Custom adapter spawns subprocess and manages JSON-RPC protocol
3. **Standalone mode**: Configured via `lfs.standalonetransferagent = proton`; adapter receives `action: null` and must determine file locations independently

### Protocol Messages
**Init**: `{"event":"init","operation":"upload|download","remote":"origin","concurrent":true,"concurrenttransfers":4}`

**Upload Request**: `{"event":"upload","oid":"...",​"size":1000,"path":"/repo/file","action":{...}}`
**Download Request**: `{"event":"download","oid":"...","size":1000,"action":{...}}`

**Complete Response**: `{"event":"complete","oid":"...","path":"/tmp/file"}` (for downloads) or `{"event":"complete","oid":"..."}` (for uploads)
**Error Response**: `{"event":"complete","oid":"...","error":{"code":500,"message":"..."}}`

*See [custom-transfer-protocol.md](../docs/architecture/custom-transfer-protocol.md) for detailed protocol specification.*

## Integration Points
- **Authentication**: Adapter obtains credentials via git-credential helper, environment variables, or direct Proton auth flow. Session management is adapter responsibility.
- **SDK subprocess**: Adapter spawns `proton-drive-cli bridge <command>` as a subprocess with JSON stdin/stdout. No intermediate HTTP bridge.
- **File organization**: Proton Drive: `LFS/00/abc123..., LFS/01/def456..., ...` (hierarchical by OID prefix)
- **Encryption**: SDK handles client-side AES-256 encryption; adapter passes plain bytes to SDK, receives encrypted from Proton
- **Dependencies**: Minimal Go standard library (no external deps); proton-drive-cli (TypeScript) for SDK operations

## Security
- SDK is pre-production: validate authentication flows before production deployment
- Credential helpers: Integrate with Git's native credential systems (see [SECURITY.md](../submodules/git-lfs/SECURITY.md))
- Never hardcode auth tokens; always use secure credential providers (git-credential, environment, SSH agent)
- App version header: `x-pm-appversion: external-drive-protonlfs@v1.0.0` required by Proton SDK
- Event-based sync: Subscribe to Proton Drive events; do NOT poll file listings
- Session tokens: Short-lived (typically hours/days); implement refresh before expiry

## Quality Criteria & Testing Requirements

**CRITICAL: Every module, function, and public API MUST have comprehensive test coverage with unit, integration, and end-to-end tests before merging.**

### Code Quality Standards

**Go Code**:
- Format with `goimports` (enforced by Makefile)
- Lint with `golangci-lint` (see `.golangci.yml`)
- 80%+ test coverage required for new code
- No unhandled errors (use explicit error handling)
- Document all exported functions with godoc comments
- Use `testify` for assertions (require/assert patterns)
- Run unit tests before commit: `make test-adapter`

**TypeScript Code** (proton-drive-cli):
- Format with Prettier (auto-fixed by pre-commit)
- Lint with ESLint (enforced in pre-commit)
- 80%+ test coverage required for new code
- Document all exported functions with TSDoc
- Use Jest for testing (configured in `package.json`)
- Run tests before commit: `make test`

### Testing Pyramid

For every module/package, implement:

1. **Unit Tests** (70% of tests)
   - Test individual functions in isolation
   - Mock external dependencies (proton-drive-cli subprocess, filesystem, network)
   - Test happy path and error conditions
   - Test edge cases and boundary conditions
   - Examples:
     - `cmd/adapter/main_test.go` - Adapter message handling
     - `submodules/proton-drive-cli/src/` - Bridge command tests
   - Files: `*_test.go` (Go) or `*.test.ts` (TypeScript)

2. **Integration Tests** (20% of tests)
   - Test component interactions
   - Use test fixtures and mock services
   - Verify adapter ↔ proton-drive-cli subprocess communication
   - Use in-memory database or temporary files
   - Examples:
     - Adapter ↔ proton-drive-cli subprocess calls
     - Session management with file operations
   - Location: `tests/integration/` (documented in `docs/testing/integration-testing.md`)

3. **End-to-End Tests** (10% of tests)
   - Test complete workflows through all layers
   - Use real Git LFS workflow
   - Verify actual file encryption/decryption
   - Test against staging Proton account (not production)
   - Examples:
     - Full push/pull workflow
     - Concurrent file transfers
     - Error recovery and retries
   - Location: `tests/e2e/` (documented in `docs/testing/integration-testing.md`)

### Test Requirements by Phase

**Phase 3 (Custom Adapter)**:
- [ ] Unit tests for message parsing (`*_test.go`)
- [ ] Unit tests for session initialization
- [ ] Unit tests for error handling
- [ ] Integration test stub connecting adapter to proton-drive-cli
- [ ] 80%+ coverage: `go test -cover ./cmd/adapter/...`

**Phase 4 (Bridge Subprocess)**:
- [ ] Unit tests for bridge client (`bridge.go`)
- [ ] Unit tests for subprocess JSON protocol
- [ ] Integration tests: adapter ↔ proton-drive-cli subprocess
- [ ] 80%+ coverage for new code

**Phase 5 (Integration)**:
- [ ] Full adapter ↔ Git LFS integration test
- [ ] E2E test: push large file to Proton Drive
- [ ] E2E test: clone and download large file
- [ ] E2E test: concurrent transfers (4+ files)
- [ ] Performance benchmark: throughput, latency, memory

### Pre-commit Hooks

Before code is committed, MUST pass:
- [ ] Format check (goimports/prettier)
- [ ] Lint check (golangci-lint/ESLint)
- [ ] Build check (adapter builds without errors)
- [ ] Unit tests pass (100% pass rate)
- [ ] Coverage check (80%+ for new code)
- [ ] Documentation updated (per Documentation Sync Checklist)

**Setup**: Run `make install-hooks` to configure pre-commit hooks

### Test Execution

```bash
# Pre-commit (automated):
make test-adapter      # Unit tests + coverage
make fmt lint          # Format + lint

# Local testing:
make test              # All Go adapter tests
make test          # proton-drive-cli bridge tests

# CI/CD (GitHub Actions):
# Automatically runs on push/PR
# See .github/workflows/*.yml
```

### Documentation + Testing Checklist

When adding new code:

- [ ] **Unit tests written** (80%+ coverage)
- [ ] **Integration tests written** (if cross-module interaction)
- [ ] **E2E tests written** (if user-facing feature)
- [ ] **All tests passing** (`make test`)
- [ ] **Code formatted** (auto-fixed by pre-commit)
- [ ] **Code linted** (no warnings)
- [ ] **Documentation** updated:
  - Architecture diagram if design changed
  - Function-level JSDoc/godoc comments
  - Example usage in module README
  - Integration points documented
  - Error handling documented
- [ ] **CHANGELOG entry** added (time-of-writing)

## Documentation Maintenance

**Critical: All discussions, refinements, and decisions must be reflected in documentation to keep it current.**

### Documentation Files & Update Frequency
- **[docs/README.md](../docs/README.md)** - Navigation & overview (update when adding/renaming docs)
- **[docs/architecture/overview.md](../docs/architecture/overview.md)** - System design, data flows (update when architecture changes)
- **[docs/reference/git-lfs-spec-scope.md](../docs/reference/git-lfs-spec-scope.md)** - Git LFS essentials (reference only; rarely changes)
- **[docs/architecture/custom-transfer-protocol.md](../docs/architecture/custom-transfer-protocol.md)** - JSON-RPC protocol spec (update when protocol changes)
- **[docs/architecture/proton-sdk-bridge.md](../docs/architecture/proton-sdk-bridge.md)** - SDK usage & constraints (update when SDK APIs change)
- **[docs/operations/adapter-configuration.md](../docs/operations/adapter-configuration.md)** - Best practices & implementation guide (update when patterns change)
- **[docs/architecture/git-lfs-submodule.md](../docs/architecture/git-lfs-submodule.md)** - Git LFS API details & code locations (update when submodule APIs change)
- **[docs/operations/deployment.md](../docs/operations/deployment.md)** - Setup & troubleshooting (update for new platforms/CI systems)

### Update Triggers
When any of these occur, update relevant documentation:
1. **Architecture changes** — Add/remove components, change interfaces, modify data flow
2. **Protocol changes** — New/modified JSON messages, different field semantics
3. **SDK updates** — New APIs, breaking changes, constraint additions
4. **Deployment changes** — New platforms, updated install steps, new CI support
5. **Security findings** — New attack vectors, mitigation strategies, credential handling changes
6. **Best practice discoveries** — Performance optimizations, error handling patterns, testing strategies

### Documentation Sync Checklist
- [ ] File structure changes? Update [docs/README.md](../docs/README.md)
- [ ] Architecture modified? Update [docs/architecture/overview.md](../docs/architecture/overview.md) with diagrams and flow examples
- [ ] Protocol changed? Update [docs/architecture/custom-transfer-protocol.md](../docs/architecture/custom-transfer-protocol.md) with message format examples
- [ ] SDK usage changed? Update [docs/architecture/proton-sdk-bridge.md](../docs/architecture/proton-sdk-bridge.md) with new API calls
- [ ] Implementation patterns discovered? Update [docs/operations/adapter-configuration.md](../docs/operations/adapter-configuration.md) with examples
- [ ] Git LFS submodule APIs changed? Update [docs/architecture/git-lfs-submodule.md](../docs/architecture/git-lfs-submodule.md) with new types/functions/line numbers
- [ ] Build/deploy steps changed? Update [docs/operations/deployment.md](../docs/operations/deployment.md) with new commands
- [ ] Update this file? Yes — reflects current best practices and project conventions

### Key References
When implementing features, consult:
- **Git LFS API & submodule details**: [docs/architecture/git-lfs-submodule.md](../docs/architecture/git-lfs-submodule.md) (specific types, functions, line numbers)
- **Protocol questions**: [submodules/git-lfs/docs/custom-transfers.md](../submodules/git-lfs/docs/custom-transfers.md)
- **Git LFS API**: [submodules/git-lfs/docs/api/](../submodules/git-lfs/docs/api/)
- **Adapter registration**: `submodules/git-lfs/tq/manifest.go` (configureCustomAdapters)
- **Custom adapter implementation**: `submodules/git-lfs/tq/custom.go` (subprocess communication)
- **proton-drive-cli**: `submodules/proton-drive-cli/`
