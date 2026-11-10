# Contributing

Proton LFS stores Git LFS objects in Proton Drive. Bug reports, fixes and
documentation improvements are welcome. Changes to the Proton Drive client
itself belong to [proton-drive-cli](https://github.com/SevenOfNine-labs/proton-drive-cli),
which is a submodule here.

## Reporting

- Bugs and feature requests: [open an issue](https://github.com/SevenOfNine-labs/proton-lfs-cli/issues/new/choose).
  Attach `proton-lfs-tray status` and `proton-drive-cli doctor --json`;
  neither holds a secret.
- Vulnerabilities: follow [SECURITY.md](SECURITY.md), never a public issue.
- Never paste a session token, key password, `pass-cli` output or a
  credential-store entry into an issue or pull request.

## Development Setup

Go 1.25 or later, git-lfs, Node.js 22 or later, Yarn 4 through Corepack
(Node.js 25 and later ship without Corepack: `npm install -g corepack`),
[golangci-lint](https://golangci-lint.run/) 2.8 and
[pre-commit](https://pre-commit.com/):

```bash
git clone --recurse-submodules https://github.com/SevenOfNine-labs/proton-lfs-cli.git
cd proton-lfs-cli
corepack enable
make setup           # .env, Go modules, proton-drive-cli dependencies
make build-all
make install-hooks   # pre-commit and commit-msg hooks (needs pre-commit)
```

[Deployment](docs/operations/deployment.md) covers the toolchains, and the
[Architecture](docs/architecture/architecture.md) the components.

## Checks

CI runs these on every pull request; run them before you push:

```bash
make test                  # Go unit tests with the race detector
make test-integration      # Git LFS integration suites (needs git-lfs)
make fmt-check lint        # gofmt, go vet, golangci-lint, comment policy
make docs                  # markdownlint
./scripts/check-doc-links.sh
(cd submodules/proton-drive-cli && corepack yarn build && corepack yarn test --maxWorkers=2)
```

Every suite is offline: the Proton API, pass-cli and proton-drive-cli are
mocked. Real-account checks are separate, guarded canaries
([Live Canary Runbook](docs/operations/live-canary-runbook.md)) that never
run in CI.

## Code and Documentation

- Prefer the Go standard library or a maintained library to new code for
  anything a standard or a common package covers.
- Comments cite a standard, a specification or external documentation, or
  state an invariant, in at most three lines; docs and comments describe the
  current behavior only. [CLAUDE.md](https://github.com/SevenOfNine-labs/proton-lfs-cli/blob/main/CLAUDE.md)
  has the full comment policy; `scripts/check-comment-policy.sh` enforces
  its vocabulary.
- Bridge requests never carry account credentials, and the adapter never
  logs in ([Threat Model](docs/security/threat-model.md)).
- Update `USAGE.md` and `docs/` with every change to flags, environment
  variables, files or the tray.

## Commits and Pull Requests

- [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
  `<type>(<scope>): <subject>`, header at most 72 characters, subject not
  capitalized and without a final period. Types: `feat`, `fix`, `docs`,
  `style`, `refactor`, `test`, `chore`, `ci`, `perf`, `build`. A breaking
  change adds `!` after the type or scope and a `BREAKING CHANGE:` footer.
  `commitlint.config.mjs` holds the rules; the pull request title follows
  them as well, since pull requests are squash-merged.
- Sign off every commit (`git commit -s`) to certify the
  [Developer Certificate of Origin](https://developercertificate.org/), and
  sign it (`-S`) when you can.
- One logical change per pull request, with the test evidence in its
  description. `main` accepts changes only through pull requests whose
  checks pass.

Contributions are licensed under the [MIT License](https://github.com/SevenOfNine-labs/proton-lfs-cli/blob/main/LICENSE).
