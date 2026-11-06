# Integration Testing

## Scope

Integration tests validate Git LFS client behavior against the adapter runtime and backend implementations.

## Test Commands

| Command | Scope |
| --- | --- |
| `make test` | Go unit tests (`./cmd/...`, `./internal/...`) |
| `cd submodules/proton-drive-cli && yarn test` | proton-drive-cli Jest tests (CI runs them on Node.js 22 and 26) |
| `make test-integration` | Git LFS + adapter integration suite |
| `make test-integration-adapter-exit` | Adapter exits mid-transfer without completing |
| `make test-integration-stress` | High-volume concurrent stress/soak |
| `make test-integration-sdk` | sdk backend through a built proton-drive-cli and your saved session (`PROTON_LFS_RUN_SDK_INTEGRATION=1`) |
| `make test-integration-failure-modes` | Failure mode tests (wrong OID, crash, exit without response) |
| `make test-integration-config-matrix` | Direction config matrix tests |
| `make test-integration-credentials` | Credential flow security tests |
| `make test-e2e-mock` | Mocked E2E pipeline (no real credentials) |
| `make live-canary-preflight` | Offline gate before any real Proton canary |
| `make live-drive-scope-canary` | Guarded read-only Drive metadata canary; no transfer |
| `make browser-fork-canary` | Guarded one-login browser-fork canary; no transfer |
| `make test-e2e-real` | Guarded real Proton Drive E2E; requires the live canary acknowledgement |

## Prerequisites

- `git-lfs` available on `PATH`.
- Adapter built with `make build`.
- For SDK path: Node.js installed and `proton-drive-cli` built (`make build-drive-cli`).

## Credentials For SDK Tests

Proton account authorization is browser-fork-only and must already have
produced a saved local session before real SDK transfers run. proton-drive-cli
reads the session's key password from the provider recorded in the session
(`pass-cli` or `git-credential`); account usernames, passwords and
second-factor codes are never supplied to Git LFS transfers.

Preferred path:

```bash
pass-cli login
make test-integration-sdk
```

`make test-integration-sdk` runs `make check-submodules` and
`make check-sdk-prereqs` (git-lfs on `PATH`, a built proton-drive-cli entry
point, a resolvable node binary and, when the key-password provider
`PROTON_KEY_PASSWORD_PROVIDER` is `pass-cli` (the default), a logged-in
pass-cli), then, for `pass-cli`, exports `PROTON_PASS_CLI_BIN` via
`scripts/export-pass-env.sh` and runs the tests matching `SDK` with
`PROTON_LFS_RUN_SDK_INTEGRATION=1`. The script prints only that one export.
If Node is only configured via shell startup files (`~/.zshrc`, `nvm`, `fnm`), run with:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

Sign in once with `proton-drive-cli login --key-password-provider <provider>`
before real SDK tests; two-factor authentication is completed in the browser.
The browser fork delivers the key password for every account type, and
transfers unlock Drive with the stored UID-scoped key password.

## Personal Account Practical Steps

If you are testing with a personal Proton account:

1. With the `pass-cli` key-password provider, authenticate pass-cli; with
   `git-credential`, set `PROTON_KEY_PASSWORD_PROVIDER=git-credential`. Then
   complete the browser-fork login and run the prerequisite checks:

```bash
pass-cli login
proton-drive-cli login --key-password-provider pass-cli
make check-sdk-prereqs
```

1. Run the sdk suite, which performs one real Proton Drive round trip per
   test through `submodules/proton-drive-cli/dist/index.js`
   (`tests/integration/git_lfs_sdk_backend_test.go`):

   ```bash
   make test-integration-sdk
   ```

   CI runs the same tests against `tests/testdata/mock-proton-drive-cli.js`
   (`bridge-integration` job) and never against Proton.

## Mocked E2E Testing

For CI and local testing without real Proton credentials:

```bash
make test-e2e-mock
```

This uses `tests/testdata/mock-proton-drive-cli.js` (CI also points
`PROTON_PASS_CLI_BIN` at `scripts/mock-pass-cli.sh`) to exercise the full
pipeline: `git lfs push` -> adapter -> mock proton-drive-cli -> mock storage,
then clone and pull back. A second test sets the mock's auth state to
`needs_key_password` and checks that the push fails with the key-password
error.

## Coverage Expectations

- Real `git-lfs` subprocess path for upload and download.
- proton-drive-cli bridge contract path covering auth-state, init, exists, upload and download.
- Standalone mode behavior (`action: null`) coverage.
- Object-level failure handling coverage (`complete.error`).
- Wrong-OID response rejection coverage (`progress` and `complete`).
- Adapter crash and no-response subprocess failure coverage.
- Adapter exits mid-transfer coverage across the OS CI matrix: git-lfs fails when the adapter closes stdout (it has no activity timeout for custom adapters; `lfs.activitytimeout` applies to its HTTP client only).
- Concurrent multi-file roundtrip coverage (`lfs.customtransfer.proton.concurrent=true`).
- High-volume concurrent stress/soak coverage (`PROTON_LFS_STRESS_*`).
- Mocked E2E pipeline coverage (full Git LFS push/pull through mock proton-drive-cli).

## Real Proton Canary

Do not run real-account tests directly. First follow
`docs/operations/live-canary-runbook.md`.

`make test-e2e-real` refuses to run unless this acknowledgement is set for the
same command and the offline doctor arguments are supplied. It also depends on
`make live-drive-scope-canary`, which performs one read-only `bridge list` call
against `/` before any Git LFS transfer can start, and it requires a
logged-in pass-cli (`scripts/export-pass-env.sh`):

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--key-password-provider pass-cli" \
  make test-e2e-real
```

The Go test also checks these gates directly, so a direct
`go test -tags integration ... -run E2EReal` invocation skips unless the
same live-canary environment is present. When that environment is present, the
direct Go path runs the offline doctor (requiring `ok` and
`canAttemptLiveCanary`) and the read-only Drive scope canary before creating a
Git repository, LFS object, or transfer adapter. The Makefile and Go test both
parse the structured `doctor --json` readiness fields instead of matching
free-form doctor output.
Offline readiness is deliberately local: it proves session shape, local
expiry, file permissions, and that the session's key-password entry is
readable. It does not prove a remotely revoked or server-expired Proton
session is still valid; that can only be found by the guarded live metadata
path, and failures there must not cause a retry-login loop or continue into
the transfer path.
The live path also classifies Proton API 9101 as `insufficient_scope`, which is
an app/session authorization-scope blocker rather than a credential blocker.
Use `proton-lfs-tray scope-diagnostics` for redacted local evidence, and add
`--live` with the explicit canary acknowledgement for exactly one read-only
metadata probe. This command is designed for issue evidence: it reports local
scope names and structured Proton error details without raw tokens, passwords,
or UIDs.

`make browser-fork-canary` is a separate live-login path. It requires
`PROTON_LFS_LIVE_CANARY`, `LIVE_CANARY_DOCTOR_ARGS`, and
`LIVE_BROWSER_FORK_LOGIN_ARGS`, runs one browser-fork-only `login`, then
only inspects local status and offline doctor readiness. Its post-login doctor
inspection requires `ok`, `authState.state` `ready` and `canAttemptTransfer`,
and its script tests (`scripts/browser_fork_canary_test.go`) prove that no
transfer command is attempted in this path. The script accepts only
`--key-password-provider` and `--key-password-host` login args, so account
credential flags and unknown options fail before proton-drive-cli is
invoked.

## High-Value Missing Tests

- Real Proton API integration tests remain gated behind the live canary
  runbook and should never run in CI.

## Stress Tuning

`make test-integration-stress` supports optional scale controls:

- `PROTON_LFS_STRESS_FILE_COUNT` (default `24`)
- `PROTON_LFS_STRESS_ROUNDS` (default `3`)
- `PROTON_LFS_STRESS_CONCURRENCY` (default `8`)
