# Current State

Date: 2026-11-06

## Implemented

- Adapter protocol loop (`init`, `upload`, `download`, `terminate`) is implemented and testable.
- Local backend is usable for deterministic end-to-end integration tests.
- SDK backend path is wired and covered with integration tests against `proton-drive-cli` (direct subprocess).
- The Go adapter spawns `proton-drive-cli bridge <command>` directly via stdin/stdout JSON, replacing the former Node.js HTTP bridge layer.
- SDK integration suite covers the push and clone round trip (auth-state,
  init, exists, upload, download); mocked auth-state blockers and
  large-object progress semantics are covered without allocating large
  payloads.
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
- Security hardening: OID validation, path traversal prevention, one bridge subprocess per command with a size-scaled timeout (`--bridge-timeout`, default 5 min, plus 1 s per 256 KiB) and process-group kill on expiry.
- Security tests: command injection, rate limiting, credential flow, session file permissions.
- Submodule pins are checked with `make check-submodules` instead of relying on
  recursive SDK traversal, because the official Proton SDK commit currently
  contains an unmapped nested gitlink.
- Latest official Proton Drive CLI/SDK auth signals were reviewed on
  2026-06-25. Current official sources still require browser-based CLI login,
  honest third-party `x-pm-appversion` identity, SDK-based Drive operations
  rather than raw API calls, and application-owned auth/session handling.
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
- Bridge subprocess tests cover strict envelopes, size-scaled timeouts,
  malformed output, stderr redaction, and environment filtering.
- Helper CLI scope diagnostics provide redacted local auth/session evidence and
  an explicitly acknowledged one-read live Drive scope probe for API 9101
  investigations.
- Tray refresh now consumes structured `proton-drive-cli session refresh --json`
  results. Recoverable refresh failures back off; non-recoverable refresh-token
  rejection is shown as reconnect required and is not retried for the same
  saved session.
- The vendored Proton Drive SDK was updated to `js/v0.23.0` on 2026-11-05
  (`docs/history/proton-sdk-update-2026-11-05.md`). Its dist is now ESM
  and upstream ships no npm lockfile, so `make build-sdk` builds it with
  `npm install --no-package-lock`. proton-drive-cli now reports the pinned SDK
  release in `x-pm-drive-sdk-version`, validates `x-pm-appversion` against
  Proton's published third-party grammar, dates OpenPGP operations with the
  Proton server clock, verifies address-key token signatures with the user
  keys, and logs Proton's SDK update-required notices.

## Architecture

```
Go Adapter -> proton-drive-cli subprocess (stdin/stdout JSON)
             pre-existing browser-fork session only
```

- **No .NET SDK or Node.js HTTP bridge required.** The Go adapter spawns `proton-drive-cli bridge <command>` directly.
- The former `proton-lfs-bridge` Node.js HTTP layer has been removed.

## Open Work

- Real-account canary validation with a disposable account after mocked
  auth/session gates stay green and explicit canary acknowledgement is set.
- Production observability baseline (metrics, SLOs, alerts, runbooks).
- SDK streaming transfer progress and resume support where the SDK can expose
  it. The local backend streams progress during filesystem copies.

Open items carried over from the SDK update audit of 2026-11-05
(`docs/history/proton-sdk-update-2026-11-05.md`):

- Proton's cryptographic model migration (announced for end of 2026 / early
  2027): keep the SDK pin current and act on the
  `Proton Drive requires SDK ... or newer` notices that proton-drive-cli now
  logs from the `x-pm-drive-requirements` headers.

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

SDK integration path (one real Proton Drive round trip per test):

```bash
proton-drive-cli login --key-password-provider pass-cli
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
`proton-lfs-tray scope-diagnostics` can be used before those live runs to collect
redacted local evidence; `scope-diagnostics --live` is gated by the same
acknowledgement and performs one read-only Drive metadata probe.
If the tray log shows Proton `10013` / `Invalid refresh token`, the current
saved session cannot be repaired by refresh; run the guarded browser-fork
connect flow to create a new session.
