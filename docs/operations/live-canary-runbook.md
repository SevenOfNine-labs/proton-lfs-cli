# Live Proton Canary Runbook

This runbook is the first allowed path for touching a real Proton account.
It is intentionally narrow. Do not use it for load testing, regression
testing, CI, or exploratory debugging.

## Purpose

The canary answers only these questions:

- Can one browser-fork login complete without retry loops?
- Is a session saved only after the browser approval and the credential
  store write complete?
- Can one tiny Drive metadata read succeed from that saved session?
- Do auth challenges stop cleanly instead of escalating into repeated login
  attempts?

No upload, download, stress, or concurrency test belongs in this run.

## Preconditions

All of these must pass before the canary:

```bash
make live-canary-preflight
```

The preflight builds the adapter and proton-drive-cli (after
`make check-submodules`), runs `go test ./...`, proton-drive-cli's
`yarn lint` and its Jest suites `src/auth/flow-safety.test.ts` and
`src/cli/doctor.test.ts`. It performs no Proton login, token refresh, upload
or download.

To include the offline doctor, pass its arguments:

```bash
LIVE_CANARY_DOCTOR_ARGS="--credential-store git-credential" \
  make live-canary-preflight
```

With `LIVE_CANARY_DOCTOR_ARGS` set, the preflight runs
`proton-drive-cli doctor --json $LIVE_CANARY_DOCTOR_ARGS` and pipes the
report through `scripts/check_doctor_readiness.go --require-live-canary`,
which validates `authState.state` against the bridge auth-state schema and
requires `ok` and `canAttemptLiveCanary` (`authState.state` is `ready` or
`needs_login`). `state=ready` means the saved session has every field
`proton-drive-cli login` writes, its access token has not expired locally,
and its entry is readable from the recorded credential store. It does
not prove the server still accepts the session; remote revocation or
server-side expiry is discovered only by the guarded live metadata read, and
must not trigger an automatic retry login.

## Account Rules

- Use a disposable or low-risk Proton account.
- Initialize Proton Drive once in the web UI before this run.
- Do not use a primary personal account for the first canary.
- Do not run from CI.
- Do not run multiple terminals, tray refresh, or LFS transfers in parallel.
- Let `proton-drive-cli login` store the session tokens and the
  browser-fork key password through `git-credential` or `pass-cli`; never
  put a password in an environment variable or a command line.

## Hard Stop Conditions

Stop immediately if any of these appear:

- CAPTCHA or human verification.
- HTTP 429 or Proton anti-abuse/rate-limit code (for example 2028).
- Proton API 9101 / `INSUFFICIENT_SCOPE`.
- Unexpected second login prompt.
- More than one browser-fork login attempt in logs.
- Credentials unavailable or a key password that does not unlock the
  account keys (`credentials_unavailable` after a successful login).
- Any log line containing a raw password or token.

After a hard stop, do not retry live. Return to mocked fixtures and add a
regression test for the observed state.
`INSUFFICIENT_SCOPE` means the saved session can be locally ready while Proton
still rejects the app/session authorization scope for Drive API calls. Treat it
as an app-version/session-scope investigation, not as a credential problem.
For redacted evidence, run `proton-lfs-tray scope-diagnostics` first, then
`PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT proton-lfs-tray scope-diagnostics --live`
if one read-only server probe is intentionally allowed. The diagnostics command
does not run login or transfer commands.

## One Login Attempt

Set an explicit acknowledgement only for the commands that may touch Proton:

```bash
export PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT
```

The guarded form is the browser-fork canary below. The manual form is
exactly one login:

```bash
proton-drive-cli login --credential-store git-credential
```

For Proton Pass:

```bash
proton-drive-cli login --credential-store pass-cli
```

Complete the sign-in, including two-factor authentication, in the browser.
If it fails, stop. Do not immediately run the command again.

## Session Inspection

After login, inspect only local state:

```bash
proton-drive-cli status
proton-drive-cli doctor
```

Without options the doctor verifies the credential store and host
recorded in the session. Expected local properties:

- Session file exists under `~/.proton-drive-cli/session.json`.
- Session file mode is owner-only (`600`).
- Session JSON contains `uid`, `credentialStore`, `credentialHost`,
  `revision`, and `scopes` and `tokenExpiresAt` when Proton reported them.
- Session JSON contains no token or password.

## Browser-Fork Canary

The guarded login canary runs exactly one `login`, then local `status` and
offline `doctor --json` inspection. It does not upload, download, or start
the Git LFS real E2E transfer.

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-store git-credential" \
LIVE_BROWSER_FORK_LOGIN_ARGS="--credential-store git-credential" \
  make browser-fork-canary
```

For Proton Pass, name `pass-cli` in both variables:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-store pass-cli" \
LIVE_BROWSER_FORK_LOGIN_ARGS="--credential-store pass-cli" \
  make browser-fork-canary
```

The target refuses to start without the acknowledgement, without
`LIVE_CANARY_DOCTOR_ARGS` or without `LIVE_BROWSER_FORK_LOGIN_ARGS`, then
runs `make live-canary-preflight` (whose doctor gate accepts `needs_login`
before the login) and `scripts/browser-fork-canary.sh`. The script accepts
only `--credential-store` and `--credential-host` (also in
`--option=value` form) in `LIVE_BROWSER_FORK_LOGIN_ARGS`, and only tokens of
letters, digits and `._=:/@,+-` in either variable; anything else fails
before proton-drive-cli is invoked. After the login it requires the doctor
report to have `ok`, `authState.state` `ready` and `canAttemptTransfer`
(`check_doctor_readiness.go --require-state ready --require-transfer`).

Stop after this target and record the result before any metadata read.

## One Metadata Read

Run the guarded metadata-read target:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-store pass-cli" \
  make live-drive-scope-canary
```

This target requires the acknowledgement and `LIVE_CANARY_DOCTOR_ARGS`, runs
the offline preflight with the doctor gate first, then
`scripts/live-drive-scope-canary.sh`, which sends exactly one read-only
`bridge list` request for `/`. The script exits 0 when the request succeeds,
3 on `INSUFFICIENT_SCOPE` / Proton API 9101 and 2 on any other failure. It
does not run `login`, `init`, upload, download, delete, or the Git LFS
transfer path.

The manual equivalent is one small metadata read:

```bash
proton-drive-cli ls /
```

Stop after this command regardless of success. Do not upload or download in the
same canary session. If the target reports Proton API 9101 /
`INSUFFICIENT_SCOPE`, do not continue to `make test-e2e-real`.

## Real E2E Guard

The `test-e2e-real` target is guarded: it depends on
`make live-drive-scope-canary`, so it refuses to run without the explicit
acknowledgement and the offline doctor arguments, and the Git LFS transfer
cannot start until one read-only Drive metadata request succeeds. When the
session's credential store is pass-cli (`CREDENTIAL_STORE`, default
`$PROTON_CREDENTIAL_STORE` or `pass-cli`), it then exports
`PROTON_PASS_CLI_BIN` through `scripts/export-pass-env.sh`, which requires
pass-cli to be installed and logged in; with
`CREDENTIAL_STORE=git-credential` pass-cli is not needed. It runs the
integration tests matching `E2E`:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-store pass-cli" \
  make test-e2e-real
```

`TestE2ERealProtonDrivePipeline` checks the same gates itself, so a direct
`go test` invocation skips unless the acknowledgement and
`LIVE_CANARY_DOCTOR_ARGS` are set and proton-drive-cli is built. It runs
`doctor --json` with those arguments and parses it with `internal/preflight`
(requiring `ok` and `canAttemptLiveCanary`), then performs the same
read-only `bridge list` canary and stops hard on any failure before it
creates a Git repository, LFS object, or transfer adapter.

The test pushes one tiny `proton-lfs.canary` LFS object under a unique
`LFS/canary/proton-lfs-cli/<UTC timestamp>-<pid>` storage base unless
`PROTON_LFS_CANARY_STORAGE_BASE` is set, pulls it into a fresh clone and
compares the bytes. It attempts a final `bridge delete` for that single OID
after verification, logs only the OID prefix, and treats cleanup failure as
evidence to inspect rather than a reason to retry live immediately.

Do not run this target until the one-login canary and one metadata read have
succeeded and the result has been recorded.

## Evidence To Record

Record only redacted evidence:

- Date and local branch/commit.
- Account class, for example a disposable account.
- Which credential store was used.
- Whether two-factor authentication was required in the browser.
- Whether `doctor` passed after login.
- Whether one `ls /` metadata read passed.
- Any hard stop condition.

Never paste tokens, raw session JSON, passwords, two-factor codes, or full
account email addresses into issue trackers or commits.

## Cleanup

After the canary:

```bash
proton-drive-cli logout
unset PROTON_LFS_LIVE_CANARY
```

`logout` removes the credential-store entry, revokes the session and deletes the
local session files. If a hard stop exposed secrets in logs, rotate the
affected credential before trying again.
