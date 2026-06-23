# Integration Testing

## Scope

Integration tests validate Git LFS client behavior against the adapter runtime and backend implementations.

## Test Commands

| Command | Scope |
| --- | --- |
| `make test` | Adapter unit tests |
| `make test-sdk` | proton-drive-cli unit tests |
| `make test-integration` | Git LFS + adapter integration suite |
| `make test-integration-timeout` | Stalled-adapter timeout semantics |
| `make test-integration-stress` | High-volume concurrent stress/soak |
| `make test-integration-sdk` | SDK backend integration path (local service by default) |
| `make test-integration-failure-modes` | Failure mode tests (wrong OID, crash, hang) |
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

Credentials are resolved by proton-drive-cli via the configured provider (`pass-cli` or `git-credential`).
Only local unlock material is resolved this way. Proton account authorization is
browser-fork-only and must already have produced a saved local session before
real SDK transfers run; account usernames, passwords, and second-factor codes
are not supplied to Git LFS transfers.

Preferred path:

```bash
pass-cli login
make test-integration-sdk
```

`make test-integration-sdk` now performs a prerequisite check and may export
`PROTON_PASS_CLI_BIN` via `scripts/export-pass-env.sh` for sessions whose
key-password provider is Proton Pass. It does not export account usernames or
passwords.
If Node is only configured via shell startup files (`~/.zshrc`, `nvm`, `fnm`), run with:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

See `docs/architecture/sdk-capability-matrix.md` for the full environment matrix.

Accounts requiring 2FA should complete interactive `proton-drive login` before
real SDK tests. Browser-fork sessions normally unlock Drive with the stored
UID-scoped key password. Add a separate mailbox/data password provider only if
offline doctor reports `needs_data_password`:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --data-credential-provider git-credential"
```

## Personal Account Practical Steps

If you are testing with a personal Proton account:

1. Complete browser-fork login first:

```bash
proton-drive login --key-password-provider pass-cli
```

1. Authenticate pass-cli if the saved session uses it as a key-password or
   data-password provider, then run prerequisite checks:

```bash
pass-cli login
make check-sdk-prereqs
```

1. Choose one runtime path:

   - Local prototype path (no real Proton backend): `make test-integration-sdk`
   - Real backend via proton-drive-cli: `make test-integration-sdk-real`

## Mocked E2E Testing

For CI and local testing without real Proton credentials:

```bash
make test-e2e-mock
```

This uses `mock-pass-cli.sh` and `mock-proton-drive-cli.js` to exercise the full pipeline: `git lfs push` -> adapter -> mock proton-drive-cli -> mock storage, then clone and pull back.

## Coverage Expectations

- Real `git-lfs` subprocess path for upload and download.
- proton-drive-cli bridge contract path covering upload, download, list, and token refresh.
- Standalone mode behavior (`action: null`) coverage.
- Object-level failure handling coverage (`complete.error`).
- Wrong-OID response rejection coverage (`progress` and `complete`).
- Adapter crash and no-response subprocess failure coverage.
- Stalled-adapter timeout semantics coverage (`lfs.activitytimeout`) across OS CI matrix.
- Concurrent multi-file roundtrip coverage (`lfs.customtransfer.proton.concurrent=true`).
- High-volume concurrent stress/soak coverage (`PROTON_LFS_STRESS_*`).
- Mocked E2E pipeline coverage (full Git LFS push/pull through mock proton-drive-cli).

## Real Proton Canary

Do not run real-account tests directly. First follow
`docs/operations/live-canary-runbook.md`.

`make test-e2e-real` refuses to run unless this acknowledgement is set for the
same command and the offline doctor arguments are supplied. It also depends on
`make live-drive-scope-canary`, which performs one read-only `bridge list` call
against `/` before any Git LFS transfer can start:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--key-password-provider pass-cli" \
  make test-e2e-real
```

The Go test also checks these gates directly before resolving credentials, so a
direct `go test -tags integration ... -run E2EReal` invocation skips unless the
same live-canary environment is present. When that environment is present, the
direct Go path also runs the read-only Drive scope canary before creating a Git
repository, LFS object, or transfer adapter. The Makefile and Go test both
parse the structured `doctor --json` readiness fields instead of matching
free-form doctor output.
Offline readiness is deliberately local: it proves session shape, local expiry,
permissions, and unlock-provider availability. It does not prove a remotely
revoked or server-expired Proton session is still valid; that can only be found
by the guarded live metadata path, and failures there must not cause a
retry-login loop or continue into the transfer path.
The live path also classifies Proton API 9101 as `insufficient_scope`, which is
an app/session authorization-scope blocker rather than a credential or data
password blocker.

`make browser-fork-canary` is a separate live-login path. It requires
`PROTON_LFS_LIVE_CANARY`, `LIVE_CANARY_DOCTOR_ARGS`, and
`LIVE_BROWSER_FORK_LOGIN_ARGS`, runs one browser-fork-only `login`, then
only inspects local status and offline doctor readiness. Its post-login doctor
inspection requires `authMode=browser-fork`, `state=ready`, and
`canAttemptTransfer=true`, and its script tests prove that no transfer command
is attempted in this path. The script accepts only `--key-password-provider`
and `--key-password-host` login args, so legacy account credential flags and
unknown options fail before proton-drive-cli is invoked.

## High-Value Missing Tests

- Real Proton API integration tests remain gated behind the live canary
  runbook and should never run in CI.

## Stress Tuning

`make test-integration-stress` supports optional scale controls:

- `PROTON_LFS_STRESS_FILE_COUNT` (default `24`)
- `PROTON_LFS_STRESS_ROUNDS` (default `3`)
- `PROTON_LFS_STRESS_CONCURRENCY` (default `8`)
