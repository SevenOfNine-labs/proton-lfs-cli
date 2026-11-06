# Repository Guidelines

## Repository Identity

- Canonical repository name: `proton-lfs-cli` (renamed from `proton-git-lfs`).
- Keep instruction files aligned with `submodules/proton-drive-cli/AGENTS.md` and `submodules/proton-drive-cli/CLAUDE.md`.


## Project Structure & Module Organization

- `cmd/adapter/`: Go Git LFS custom transfer adapter (`main.go` protocol loop, `backend.go` local and Proton Drive backends, `bridge.go` proton-drive-cli subprocess client with JSON stdin/stdout).
- `cmd/tray/`: System tray app and `proton-lfs-tray` command-line helpers.
- `internal/config/`: Shared constants, status file and preferences; `internal/preflight/`: parser of `proton-drive-cli doctor --json` for the canary gates.
- `tests/integration/`: Black-box Git LFS integration tests for protocol, concurrency, adapter exit, failure modes, credentials, and the sdk backend.
- `scripts/`: Build, packaging, install, and canary scripts.
- `docs/`: Architecture, operations, reference, testing, security, and history docs (start at `docs/README.md`).
- `submodules/`: Upstream dependencies (`git-lfs`, `pass-cli`, `proton-drive-cli`) used for reference and integration.

## Build, Test, and Development Commands

- `make setup`: Prepare `.env` and install Go + JS dependencies.
- `make build`: Build adapter binary to `bin/git-lfs-proton-adapter`.
- `make test`: Run the Go unit tests for `./cmd/...` and `./internal/...`.
- `cd submodules/proton-drive-cli && yarn test`: Run the proton-drive-cli Jest tests.
- `make test-integration`: Run Go integration tests (`-tags integration`).
- `make check-sdk-prereqs && make test-integration-sdk`: Validate/pass-cli-driven SDK integration path.
- `make build-drive-cli`: Build the proton-drive-cli TypeScript bridge.
- `make test-e2e-mock`: Run the mocked end-to-end pipeline through `tests/testdata/mock-proton-drive-cli.js`.
- `make test-integration-credentials`: Run credential flow security tests.
- `make fmt && make lint`: Run formatting and lint checks before pushing.

## Coding Style & Naming Conventions

- Go: run `go fmt`/`go vet`; keep code idiomatic and error handling explicit.
- TypeScript: `yarn lint` (`tsc --noEmit`) and Jest in `submodules/proton-drive-cli/`.
- Tests: Go files use `*_test.go`; TypeScript tests use `*.test.ts`.
- Keep names descriptive and scoped by concern (`backend.go`, `bridge.go`, `status.go`).
- Prefer small, composable functions over large handlers.

## Testing Guidelines

- Add unit tests with every behavior change in both Go and TypeScript layers.
- Add/extend integration tests in `tests/integration/` for protocol or workflow changes.
- Target meaningful coverage on new code paths (project guidance: ~80%+ for new logic).
- For SDK backend checks, set prerequisites first (`pass-cli login` and `make build-drive-cli`).

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

## Commit & Pull Request Guidelines

- Conventional Commits are mandatory: `<type>(<scope>): <subject>` with one of `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`, `perf`, `build` (example: `feat(adapter): handle init retry`); the commit-msg hook and the Commits workflow enforce it.
- Commits are signed and signed off by their author (`git commit -s -S`) and carry no AI attribution.
- Keep PRs focused; include:
- What changed and why.
- Test evidence (`make test`, `make test-integration-sdk`, etc.).
- Docs updates when behavior/config changed.
- Never commit secrets; keep `.env` local and follow `.env.example`.
