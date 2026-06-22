# Live Proton Canary Runbook

This runbook is the first allowed path for touching a real Proton account.
It is intentionally narrow. Do not use it for load testing, regression
testing, CI, or exploratory debugging.

## Purpose

The canary answers only these questions:

- Can one interactive login complete without retry loops?
- Is a session saved only after required challenges complete?
- Can one tiny Drive metadata read succeed from that saved session?
- Do auth challenges stop cleanly instead of escalating into repeated login
  attempts?

No upload, download, stress, or concurrency test belongs in this run.

## Preconditions

All of these must pass before the canary:

```bash
make live-canary-preflight
```

If a local credential-store check is needed, run:

```bash
LIVE_CANARY_DOCTOR_ARGS="--credential-provider git-credential" \
  make live-canary-preflight
```

For a two-password account, include the separate data credential:

```bash
LIVE_CANARY_DOCTOR_ARGS="--credential-provider git-credential --data-credential-provider git-credential --require-data-password" \
  make live-canary-preflight
```

The preflight is offline by default. It runs root Go tests, the mocked auth
safety gate, the doctor tests, TypeScript lint, and a build freshness check.
It does not perform Proton SRP login or token refresh.

## Account Rules

- Use a disposable or low-risk Proton account.
- Initialize Proton Drive once in the web UI before this run.
- Do not use a primary personal account for the first canary.
- Do not run from CI.
- Do not run multiple terminals, tray refresh, or LFS transfers in parallel.
- Do not set `PROTON_DATA_PASSWORD` or `PROTON_SECOND_FACTOR_CODE`.
- Store login and mailbox/data passwords only through `git-credential` or
  `pass-cli`.

## Hard Stop Conditions

Stop immediately if any of these appear:

- CAPTCHA or human verification.
- HTTP 429 or Proton anti-abuse/rate-limit code.
- FIDO2-only challenge.
- Unexpected second login prompt.
- More than one SRP login attempt in logs.
- Key unlock failure.
- Missing or wrong mailbox/data password.
- Any log line containing a raw password, token, or TOTP code.

After a hard stop, do not retry live. Return to mocked fixtures and add a
regression test for the observed state.

## One Login Attempt

Set an explicit acknowledgement only for the command that may touch Proton:

```bash
export PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT
```

Run exactly one interactive login:

```bash
proton-drive login --credential-provider git-credential
```

For Proton Pass:

```bash
proton-drive login --credential-provider pass-cli
```

If the account requires TOTP, complete the single prompt. If the prompt fails,
stop. Do not immediately run the command again.

## Session Inspection

After login, inspect only local state:

```bash
proton-drive status
proton-drive doctor --credential-provider git-credential
```

Expected local properties:

- Session file exists under `~/.proton-drive-cli/session.json`.
- Session file mode is owner-only (`600`).
- Session JSON contains tokens and metadata only.
- Session JSON contains no password-like fields.

## One Metadata Read

Run one small metadata read:

```bash
proton-drive ls /
```

Stop after this command regardless of success. Do not upload or download in
the same canary session.

## Browser-Fork Canary

Use this only after the normal one-login and metadata-read canary path above is
understood. It is for accounts that need the browser session-fork path, such as
FIDO2-oriented accounts. The target runs exactly one
`login --auth-mode browser-fork` command, then performs local `status` and
offline `doctor --json` inspection. It does not upload, download, or start the
Git LFS real E2E transfer.

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-provider git-credential" \
LIVE_BROWSER_FORK_LOGIN_ARGS="--key-password-provider git-credential" \
  make browser-fork-canary
```

If you want the derived browser-fork key password stored in Proton Pass instead
of Git Credential Manager, make both the login args and credential environment
explicit:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
PROTON_CREDENTIAL_PROVIDER=pass-cli \
PROTON_KEY_PASSWORD_PROVIDER=pass-cli \
LIVE_CANARY_DOCTOR_ARGS="--credential-provider pass-cli" \
LIVE_BROWSER_FORK_LOGIN_ARGS="--key-password-provider pass-cli" \
  make browser-fork-canary
```

Stop after this target. Only run `make test-e2e-real` later, in a separate
command, after recording that offline doctor reported `authMode=browser-fork`,
`state=ready`, and `canAttemptTransfer=true`.

## Real E2E Guard

The `test-e2e-real` target is guarded and refuses to run unless the explicit
acknowledgement and the exact offline doctor arguments are present:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-provider pass-cli" \
  make test-e2e-real
```

For a two-password account, use the same data credential provider arguments
that passed preflight:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
LIVE_CANARY_DOCTOR_ARGS="--credential-provider pass-cli --data-credential-provider pass-cli --require-data-password" \
  PROTON_DATA_CREDENTIAL_PROVIDER=pass-cli \
  make test-e2e-real
```

The test creates one tiny `.canary` LFS object under a unique
`LFS/canary/proton-lfs-cli/<timestamp>` storage base unless
`PROTON_LFS_CANARY_STORAGE_BASE` is set. It attempts a final bridge delete for
that single OID after verification, logs only the OID prefix, and treats cleanup
failure as evidence to inspect rather than a reason to retry live immediately.

Do not run this target until the one-login canary and one metadata read have
succeeded and the result has been recorded. Direct `go test` invocations are
also gated and skip before credential resolution unless the same environment is
present.

## Evidence To Record

Record only redacted evidence:

- Date and local branch/commit.
- Account class, for example disposable single-password or disposable
  two-password.
- Which provider was used.
- Whether TOTP was required.
- Whether `doctor` passed after login.
- Whether one `ls /` metadata read passed.
- Any hard stop condition.

Never paste tokens, raw session JSON, passwords, TOTP codes, or full account
email addresses into issue trackers or commits.

## Cleanup

After the canary:

```bash
proton-drive logout
unset PROTON_LFS_LIVE_CANARY
```

If a hard stop exposed secrets in logs, rotate the affected credential before
trying again.
