# Current State

Date: 2026-06-22

## Implemented

- Adapter protocol loop (`init`, `upload`, `download`, `terminate`) is implemented and testable.
- Local backend is usable for deterministic end-to-end integration tests.
- SDK backend path is wired and covered with integration tests against `proton-drive-cli` (direct subprocess).
- The Go adapter spawns `proton-drive-cli bridge <command>` directly via stdin/stdout JSON, replacing the former Node.js HTTP bridge layer.
- SDK integration suite covers upload, download, list, token refresh, mocked
  auth-state blockers, and large-object progress semantics without allocating
  large payloads.
- Transfers call `bridge auth-state` before `bridge init` and refuse every
  non-ready state without invoking `bridge auth` or allowing SRP login.
- Shared bridge contract schemas are versioned in `proton-drive-cli` and
  verified by root Go contract tests.
- Command-specific bridge request-field rules are versioned in
  `proton-drive-cli` and root tests verify the adapter request shapes against
  them.
- Browser-fork login stores its derived key password through proton-drive-cli's
  key-password provider (`pass-cli` or `git-credential`). Git LFS transfers do
  not receive account credential selectors.
- Security hardening: OID validation, path traversal prevention, subprocess pool (max 10), per-operation timeout (5 min).
- Security tests: command injection, rate limiting, credential flow, session file permissions.
- Submodule pins are checked with `make check-submodules` instead of relying on
  recursive SDK traversal, because the official Proton SDK commit currently
  contains an unmapped nested gitlink.
- Latest official Proton Drive CLI/SDK auth signals were reviewed on
  2026-06-22. `proton-drive-cli@b97563b` now pins the migrated official SDK
  `origin/main@a3fc5e54` layout and uses the new `submodules/sdk/client/js`
  package path.
- Transfer failures preserve retryable/temporary backend metadata in status JSON
  and render it in helper/tray status surfaces without adding automatic login or
  retry loops.
- SDK uploads fail closed when the pre-upload remote `exists` check fails with
  a non-404 bridge error; `404` remains the only "missing, continue to upload"
  existence-check result.
- Large-object progress coverage includes virtual multi-GiB copy/progress tests
  that do not allocate or write large fixtures.
- Bridge batch operations are private maintenance helpers and are rejected as
  Git LFS transfer events.
- Bridge subprocess tests cover strict envelopes, typed timeouts, malformed
  output, stderr redaction, and concurrency limits.

## Architecture

```
Go Adapter -> proton-drive-cli subprocess (stdin/stdout JSON)
             pre-existing browser-fork session only
```

- **No .NET SDK or Node.js HTTP bridge required.** The Go adapter spawns `proton-drive-cli bridge <command>` directly.
- The former `proton-lfs-bridge` Node.js HTTP layer has been removed.

## Not Implemented Yet

- Real-account canary validation with a disposable account after mocked
  auth/session gates stay green and explicit canary acknowledgement is set.
- Production observability baseline (metrics, SLOs, alerts, runbooks).
- SDK streaming transfer progress and resume support where the SDK can expose
  it. The local backend streams progress during filesystem copies.

## Local Baseline

```bash
git submodule update --init submodules/git-lfs submodules/pass-cli submodules/proton-drive-cli
git -C submodules/proton-drive-cli submodule update --init submodules/sdk
make check-submodules
make setup
make build-all        # Builds Go adapter, Git LFS, and proton-drive-cli
make test
make test-integration
```

SDK integration path:

```bash
eval "$(make -s pass-env)"
make check-submodules
make test-integration-sdk
```

SDK backend with proton-drive-cli:

```bash
eval "$(make -s pass-env)"
make check-submodules
make check-sdk-prereqs
make test-integration-sdk
```

Real Proton canaries remain disabled by default. The guarded path starts with
`make live-canary-preflight`, requires `LIVE_CANARY_DOCTOR_ARGS`, and refuses
to run unless `PROTON_LFS_LIVE_CANARY` matches the exact acknowledgement in the
Makefile. The separate `make browser-fork-canary` path runs one forced
browser-fork login only, then local status and offline doctor inspection before
stopping without a transfer.
