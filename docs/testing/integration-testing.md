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
| `make test-e2e-real` | Real Proton Drive E2E (requires pass-cli login + build-drive-cli) |

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

Optional (accounts requiring explicit data password or 2FA):

- `PROTON_DATA_PASSWORD`
- `PROTON_SECOND_FACTOR_CODE`

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

## High-Value Missing Tests

- Real Proton API integration tests are runnable via `make test-integration-sdk-real` (requires pass-cli login and built proton-drive-cli).

## Stress Tuning

`make test-integration-stress` supports optional scale controls:

- `PROTON_LFS_STRESS_FILE_COUNT` (default `24`)
- `PROTON_LFS_STRESS_ROUNDS` (default `3`)
- `PROTON_LFS_STRESS_CONCURRENCY` (default `8`)
