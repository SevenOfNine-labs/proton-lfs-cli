# Current State

Date: 2026-11-06

## Implemented

- Adapter protocol loop (`init`, `upload`, `download`, `terminate`) is implemented and testable.
- Local backend is usable for deterministic end-to-end integration tests.
- SDK backend: the Go adapter spawns `proton-drive-cli bridge <command>` directly and exchanges JSON over stdin/stdout; there is no HTTP layer between them.
- SDK integration suite covers the push and clone round trip (auth-state,
  init, exists, upload, download); mocked auth-state blockers and
  large-object progress semantics are covered without allocating large
  payloads.
- Account login is browser-fork-only (`proton-drive-cli login`). The fork
  delivers the key password for every account type; proton-drive-cli stores
  it, keyed by the session UID, through its key-password provider
  (`pass-cli` or `git-credential`). Git LFS transfers receive no credentials.
- Transfers call `bridge auth-state` before `bridge init` and refuse
  `needs_login`, `session_invalid` and `needs_key_password` without starting
  a login; `session_expired` continues to `bridge init`, which refreshes the
  access token.
- Bridge requests carry only `storageBase`, `appVersion` and the command
  fields. Shared bridge contract schemas and the command-specific
  request-field rules are versioned in `proton-drive-cli`; root Go contract
  tests verify the adapter's request shapes, the auth states and the 27
  bridge error codes against them.
- A stored key password that is missing or does not decrypt the account keys
  is reported as `key_password_required` with the instruction to run
  `proton-drive-cli login`.
- An expired access token does not block transfers: `bridge init` refreshes
  it before use.
- Session lifecycle in proton-drive-cli: logout removes the key-password
  entry, revokes the session and deletes the local session files; a login
  that yields a new UID removes the previous UID's key-password entry; a
  refresh token the API rejects with a Proton error body and HTTP 4xx
  other than 408 and 429 signs the session out (session file, crypto cache and key-password entry deleted).
- Security hardening: OID validation, path traversal prevention, one bridge subprocess per command with a size-scaled timeout (`--bridge-timeout`, default 5 min, plus 1 s per 256 KiB) and process-group kill on expiry.
- Security tests: malicious OIDs never reach the bridge, secret-shaped
  environment variables are not forwarded, OID case is normalized, session
  file permissions.
- Submodule pins are checked with `make check-submodules`, which verifies the
  root-owned submodules and the Proton SDK gitlink without recursing into
  the SDK.
- Official Proton Drive CLI/SDK auth signals, last reviewed on 2026-06-25,
  require browser-based CLI login, honest third-party `x-pm-appversion`
  identity, SDK-based Drive operations rather than raw API calls, and
  application-owned auth/session handling.
- Transfer failures preserve retryable/temporary backend metadata in status JSON
  and render it in helper/tray status surfaces without adding automatic login or
  retry loops.
- SDK uploads fail closed when the pre-upload remote `exists` check fails with
  a non-404 bridge error; `404` is the only "missing, continue to upload"
  existence-check result.
- Large-object progress coverage includes virtual multi-GiB copy/progress tests
  that do not allocate or write large fixtures.
- The adapter sends only `auth-state`, `init`, `upload`, `download` and
  `exists`; batch events from git-lfs are rejected.
- Bridge subprocess tests cover strict envelopes, size-scaled timeouts,
  malformed output, stderr redaction, and environment filtering.
- Helper CLI scope diagnostics provide redacted local auth/session evidence and
  an explicitly acknowledged one-read live Drive scope probe for API 9101
  investigations.
- The tray consumes structured `proton-drive-cli session refresh --json`
  results. Recoverable refresh failures back off for one minute;
  non-recoverable failures are not retried for the same session UID.
- The vendored Proton Drive SDK is pinned to `js/v0.23.0`. Its dist is ESM
  and upstream ships no npm lockfile, so `make build-sdk` builds it with
  `npm install --no-package-lock`. proton-drive-cli reports the pinned SDK
  release in `x-pm-drive-sdk-version`, validates `x-pm-appversion` against
  Proton's published third-party grammar, dates OpenPGP operations with the
  Proton server clock, verifies address-key token signatures with the user
  keys, and logs Proton's SDK update-required notices.

## Architecture

```
Go Adapter -> proton-drive-cli subprocess (stdin/stdout JSON)
             pre-existing browser-fork session only
```

No .NET SDK and no HTTP bridge are involved: the Go adapter spawns
`proton-drive-cli bridge <command>` directly.

## Open Work

- Real-account canary validation with a disposable account after mocked
  auth/session gates stay green and explicit canary acknowledgement is set.
- Production observability baseline (metrics, SLOs, alerts, runbooks).
- SDK streaming transfer progress and resume support where the SDK can expose
  it. The local backend streams progress during filesystem copies.
- Proton's announced change of its cryptographic model (end of 2026 / early
  2027): keep the SDK pin current and act on the
  `Proton Drive requires SDK ... or newer` notices that proton-drive-cli
  logs from the `x-pm-drive-requirements` headers.

## Local Baseline

```bash
git submodule update --init submodules/git-lfs submodules/pass-cli submodules/proton-drive-cli
git -C submodules/proton-drive-cli submodule update --init submodules/sdk
make check-submodules
make setup
make build-all        # Go adapter, tray app, Git LFS, vendored Proton SDK and proton-drive-cli
make test
make test-integration
```

SDK integration path (one real Proton Drive round trip per test):

```bash
pass-cli login
proton-drive-cli login --key-password-provider pass-cli
eval "$(make -s pass-env)"
make check-sdk-prereqs
make test-integration-sdk
```

Real Proton canaries are disabled by default. The guarded path starts with
`make live-canary-preflight`, requires `LIVE_CANARY_DOCTOR_ARGS`, and refuses
to run unless `PROTON_LFS_LIVE_CANARY` matches the exact acknowledgement in the
Makefile. The separate `make browser-fork-canary` path runs one browser-fork
login only, then local status and offline doctor inspection before stopping
without a transfer.
`proton-lfs-tray scope-diagnostics` can be used before those live runs to collect
redacted local evidence; `scope-diagnostics --live` is gated by the same
acknowledgement and performs one read-only Drive metadata probe.
When Proton rejects the refresh token, proton-drive-cli signs the session out
and the tray shows `Not connected`; Connect creates a new session.
