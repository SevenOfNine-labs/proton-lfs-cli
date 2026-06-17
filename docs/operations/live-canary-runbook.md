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

## Real E2E Guard

The `test-e2e-real` target is guarded and refuses to run unless the explicit
acknowledgement is present:

```bash
PROTON_LFS_LIVE_CANARY=I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT \
  make test-e2e-real
```

Do not run this target until the one-login canary and one metadata read have
succeeded and the result has been recorded.

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
