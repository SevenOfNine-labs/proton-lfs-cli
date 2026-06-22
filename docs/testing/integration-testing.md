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
| `make browser-fork-canary` | Guarded one-login browser-fork canary; no transfer |
| `make test-e2e-real` | Guarded real Proton Drive E2E; requires the live canary acknowledgement |

## Prerequisites

- `git-lfs` available on `PATH`.
- Adapter built with `make build`.
- For SDK path: Node.js installed and `proton-drive-cli` built (`make build-drive-cli`).

## Credentials For SDK Tests

Credentials are resolved by proton-drive-cli via the configured provider (`pass-cli` or `git-credential`).

Preferred path:

```bash
pass-cli login
make test-integration-sdk
```

`make test-integration-sdk` now performs a prerequisite check and resolves `PROTON_PASS_*` via `scripts/export-pass-env.sh`.
For non-default vault/item references, export your custom `PROTON_PASS_*` values first.
If Node is only configured via shell startup files (`~/.zshrc`, `nvm`, `fnm`), run with:

```bash
make test-integration-sdk NODE="$(command -v node)"
```

See `docs/architecture/sdk-capability-matrix.md` for the full environment matrix.

Accounts requiring 2FA should complete interactive `proton-drive login` before
real SDK tests. Two-password accounts should use a separate mailbox/data
password provider entry:

```bash
git config lfs.customtransfer.proton.args "--backend=sdk --credential-provider git-credential --data-credential-provider git-credential"
```

## Personal Account Practical Steps

If you are testing with a personal Proton account:

1. Store credentials in Proton Pass (a login item with a `proton.me` URL in any vault).
1. Or export custom pass-cli configuration before tests:

```bash
eval "$(./scripts/export-pass-env.sh --ref-root 'pass://Personal/Your Entry')"
```

1. Authenticate and run prerequisite checks:

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
same command and the offline doctor arguments are supplied:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-provider pass-cli" \
  make test-e2e-real
```

The Go test also checks these gates directly before resolving credentials, so a
direct `go test -tags integration ... -run E2EReal` invocation skips unless the
same live-canary environment is present. The Makefile and Go test both parse
the structured `doctor --json` readiness fields instead of matching free-form
doctor output.

`make browser-fork-canary` is a separate live-login path. It requires
`PROTON_LFS_LIVE_CANARY`, `LIVE_CANARY_DOCTOR_ARGS`, and
`LIVE_BROWSER_FORK_LOGIN_ARGS`, runs one `login --auth-mode browser-fork`, then
only inspects local status and offline doctor readiness. Its post-login doctor
inspection requires `authMode=browser-fork`, `state=ready`, and
`canAttemptTransfer=true`, and its script tests prove that no transfer command
is attempted in this path.

## High-Value Missing Tests

- Real Proton API integration tests remain gated behind the live canary
  runbook and should never run in CI.

## Stress Tuning

`make test-integration-stress` supports optional scale controls:

- `PROTON_LFS_STRESS_FILE_COUNT` (default `24`)
- `PROTON_LFS_STRESS_ROUNDS` (default `3`)
- `PROTON_LFS_STRESS_CONCURRENCY` (default `8`)
