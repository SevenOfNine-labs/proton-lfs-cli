# Repository Guidelines

## Repository Identity

- Canonical repository name: `proton-lfs-cli` (`github.com/SevenOfNine-labs/proton-lfs-cli`).
- Keep instruction files aligned with `submodules/proton-drive-cli/AGENTS.md` and `submodules/proton-drive-cli/CLAUDE.md`.


## Project Structure & Module Organization

- `cmd/adapter/`: Go Git LFS custom transfer adapter (`main.go` protocol loop, `backend.go` local and Proton Drive backends, `bridge.go` proton-drive-cli subprocess client with JSON stdin/stdout).
- `cmd/tray/`: System tray app and `proton-lfs-tray` command-line helpers.
- `internal/config/`: Shared constants, status file and preferences; `internal/bridgeproto/`: bridge envelope parser, environment allowlist and redaction shared by adapter and tray; `internal/preflight/`: parser of `proton-drive-cli doctor --json` for the canary gates.
- `tests/integration/`: Black-box Git LFS integration tests for protocol, concurrency, adapter exit, failure modes, credentials, and the sdk backend.
- `scripts/`: Build, packaging, install, and canary scripts.
- `docs/`: Architecture, operations, reference, testing, and security docs (start at `docs/README.md`).
- `submodules/`: Upstream dependencies (`git-lfs`, `pass-cli`, `proton-drive-cli`) used for reference and integration.

## Build, Test, and Development Commands

- `make setup`: Prepare `.env` and install Go + JS dependencies.
- `make build`: Build adapter binary to `bin/git-lfs-proton-adapter`.
- `make test`: Run the Go unit tests for `./cmd/...` and `./internal/...`.
- `cd submodules/proton-drive-cli && yarn test`: Run the proton-drive-cli Jest tests.
- `make test-integration`: Run Go integration tests (`-tags integration`).
- `make check-sdk-prereqs && make test-integration-sdk`: Validate prerequisites, then run the SDK integration path against a saved browser-fork session (requires a logged-in pass-cli unless `CREDENTIAL_STORE=git-credential`).
- `make build-drive-cli`: Build the proton-drive-cli TypeScript bridge.
- `make test-e2e-mock`: Run the mocked end-to-end pipeline through `tests/testdata/mock-proton-drive-cli.js`.
- `make test-integration-credentials`: Run credential flow security tests.
- `make fmt && make lint`: Run formatting and lint checks (go vet, golangci-lint, comment policy) before pushing.

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
- For SDK backend checks, set prerequisites first (`make build-drive-cli`, one `proton-drive-cli login --credential-store <store>`, and `pass-cli login` when the store is pass-cli; the Makefile reads the store from `CREDENTIAL_STORE`).

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

## Commit & Pull Request Guidelines

- Conventional Commits are mandatory: `<type>(<scope>): <subject>` with one of `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `ci`, `perf`, `build` (example: `feat(adapter): handle init retry`); the commit-msg hook and the Commits workflow enforce it.
- Commits are signed and signed off by their author (`git commit -s -S`) and carry no AI attribution.
- Keep PRs focused; include:
- What changed and why.
- Test evidence (`make test`, `make test-integration-sdk`, etc.).
- Docs updates when behavior/config changed.
- Never commit secrets; keep `.env` local and follow `.env.example`.
