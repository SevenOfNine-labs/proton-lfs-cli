# Proton Auth Hardening Plan

**Date**: 2026-05-22
**Status**: Superseded by the browser-fork-only auth implementation. Keep this
document only for anti-abuse rationale and historical context; current source
of truth is `docs/architecture/proton-sdk-bridge.md`,
`docs/operations/live-canary-runbook.md`, and
`docs/testing/spec-requirements.yaml`.

## Scope

This historical plan covered the account authorization path used by the Git LFS adapter:

```text
git-lfs custom transfer adapter
-> Go bridge client
-> proton-drive-cli subprocess
-> auth/session/crypto adapters
-> @protontech/drive-sdk
-> Proton Drive API
```

The objective is to make auth predictable, resumable, and conservative before
any more real login attempts are made. The previous blocker was failing login
and unclear session handling; the main risk now is accidental live retry
behavior that can trigger Proton anti-abuse systems.

## External Research

Primary sources reviewed:

- Proton `go-proton-api` auth flow:
  <https://github.com/ProtonMail/go-proton-api/blob/master/manager_auth.go>
- Proton `go-proton-api` auth/session types:
  <https://github.com/ProtonMail/go-proton-api/blob/master/manager_auth_types.go>
- Proton `go-proton-api` refresh-on-401 client handling:
  <https://github.com/ProtonMail/go-proton-api/blob/master/client.go>
- Proton Bridge staged login and key unlock:
  <https://github.com/ProtonMail/proton-bridge/blob/master/internal/bridge/user.go>
- Proton Bridge CLI 2FA and mailbox-password flow:
  <https://github.com/ProtonMail/proton-bridge/blob/master/internal/frontend/cli/accounts.go>
- Proton Bridge human verification helper:
  <https://github.com/ProtonMail/proton-bridge/blob/master/internal/hv/hv.go>
- rclone Proton Drive backend docs:
  <https://rclone.org/protondrive/>
- rclone Proton Drive backend source:
  <https://github.com/rclone/rclone/blob/master/backend/protondrive/protondrive.go>
- Proton two-password mode support article:
  <https://proton.me/support/what-is-the-mailbox-password/>

Useful community precedent:

- `Proton-API-Bridge`, used by rclone's Proton Drive backend, wraps
  `go-proton-api` and models reusable login credentials, 2FA, mailbox
  password, and salted key pass as separate concerns:
  <https://github.com/henrybear327/Proton-API-Bridge>

## Research Findings

### Official Drive SDK boundary

The official JS Drive SDK is not the account login layer. It expects the caller
to provide an authenticated HTTP client, account adapter, OpenPGP module, and
SRP module. Therefore, updating the SDK submodule does not by itself fix
account login or session refresh bugs in `proton-drive-cli`.

### Proton's own auth shape

`go-proton-api` performs account login as a strict SRP sequence:

1. `POST /auth/v4/info`
2. Compute SRP client proof.
3. `POST /auth/v4`
4. Decode and verify the server proof.
5. Return a client with UID, access token, and refresh token.

It also models:

- `Auth2FAReq` for `POST /auth/v4/2fa`.
- `TwoFAInfo` with TOTP and FIDO2 states.
- `PasswordMode` with one-password and two-password modes.
- `AuthRefreshReq` for `POST /auth/v4/refresh`.
- Auth/deauth handlers so refresh token rotation can be persisted.

This matches the shape we should use in TypeScript.

### Proton Bridge's staged login pattern

Proton Bridge does not treat a successful `/auth/v4` response as "done".
Bridge separates login into:

1. `LoginAuth`: SRP login returns an authorized client that may still need
   additional steps.
2. Optional TOTP or FIDO2 authorization.
3. Optional mailbox password request when `PasswordMode` is two-password mode.
4. Key salt lookup and user key unlock.
5. User persistence only after key unlock succeeds.

Important detail: Bridge avoids deleting auth when key unlock fails, allowing
the user to retry the key password without starting a new auth sequence. That
is exactly the behavior we need to avoid extra live login attempts.

### rclone's Proton Drive precedent

rclone exposes `2fa`, `mailbox_password`, cached UID/access/refresh tokens,
cached salted key pass, and `app_version` as first-class Proton Drive config
fields. Its docs say third-party integrations should identify as
`external-drive-<project>@<version>`.

For this project, we should keep the same separation of concerns, but avoid
persisting raw passwords or TOTP codes. If we ever persist a salted key pass, it
must live in a proper encrypted local store or Proton Pass-backed credential
provider, not in the plain session JSON.

## Current Local Gaps

These gaps were found in the current `submodules/proton-drive-cli` flow:

- `src/types/auth.ts` models `2FA`, `PasswordMode`, and FIDO2 data, but
  `src/auth/index.ts` does not complete `/auth/v4/2fa`.
- `src/bridge/validators.ts` already allows `dataPassword` and
  `secondFactorCode`, but the bridge path does not use them end to end.
- `src/sdk/client.ts` accepts a single `password` and uses it both for SRP
  login and key unlock. That is wrong for two-password accounts.
- `src/sdk/client.ts` can fall back from session/key-init failure to full SRP
  login. That must be narrowed so a bad mailbox password or transient crypto
  fetch failure does not create another live login attempt.
- `src/auth/session.ts` has useful locking and atomic writes, but refresh logic
  is split across multiple call sites and token expiry is not set uniformly.
- `src/api/auth.ts` and `src/sdk/httpClientAdapter.ts` use desktop Proton Drive
  app version strings. Third-party integrations should use an external app
  version string instead.
- Docs mention some credential/session environment knobs that are not wired in
  consistently.
- The updated SDK submodule has stale JS `dist` output compared with `src`.
  The new source addition does not affect LFS auth, but the build state should
  become an explicit preflight check.

## Target Auth State Machine

Auth should be one orchestrated state machine, not logic split between
`AuthService`, `SessionManager`, `HTTPClientAdapter`, and `createSDKClient`.

```text
Start
  -> ResolveCredentials
  -> LoadSession
  -> SessionUsable?
       yes -> UnlockKeys
       no  -> LoginAllowed?
                no  -> ReturnAuthRequired
                yes -> AuthInfo
                      -> SRPProof
                      -> VerifyServerProof
                      -> HumanVerification?
                      -> TwoFactor?
                      -> DataPassword?
                      -> UnlockKeys
  -> PersistSession
  -> BuildSDKClient
  -> Ready
```

Failure states should be explicit:

- `auth_required`
- `human_verification_required`
- `two_factor_required`
- `fido2_required`
- `data_password_required`
- `key_unlock_failed`
- `rate_limited`
- `session_expired`
- `refresh_failed`
- `deauthorized`

The bridge response should surface these states as structured JSON so the Go
adapter can decide whether to stop, prompt, or report a recoverable adapter
error. It should not retry blindly.

## Session Rules

Session JSON should contain only non-password auth state:

- `schemaVersion`
- `uid`
- `sessionId`
- `userId` when known
- `userHash`
- `accessToken`
- `refreshToken`
- `scopes`
- `passwordMode`
- `twoFactorSatisfied`
- `tokenExpiresAt`
- `refreshGeneration`
- `appVersion`
- `cooldownUntil`
- `createdAt`
- `updatedAt`

It must not contain:

- login password
- mailbox/data password
- TOTP code
- raw FIDO2 assertion material

Refresh must be centralized in `SessionManager`:

1. Acquire the session file lock.
2. Reload the session inside the lock.
3. If another process already refreshed it, return that newer session.
4. Refresh with the current refresh token.
5. Persist access token, refresh token, expiry, and generation atomically.
6. Release the lock.

Every code path must use this single refresh method. No direct
`AuthApiClient.refreshToken()` calls should remain outside it.

## Credential Rules

The current implementation does not accept account-login inputs in the Git LFS
adapter or transfer bridge. Interactive account authorization is
browser-fork-only and happens before transfers.

Rules:

- Existing saved browser-fork sessions are required for normal operations.
- Git LFS transfer requests never include account username/password,
  second-factor codes, or login permission flags.
- Key-password unlock material is resolved by proton-drive-cli from the
  session's configured key-password provider.
- `PasswordMode === 2` may require a separate mailbox/data password provider;
  missing data password returns `data_password_required`.
- FIDO2, human verification, and other account challenges must stop outside the
  transfer path and must not be auto-retried.

## Anti-Abuse Rules

The safest behavior is conservative and boring:

- No automatic full-login retry loops.
- No password retry loops.
- No fallback from key-unlock failure to a new SRP login.
- No refresh attempt on rate-limit or human-verification responses.
- Retry an API request at most once after a successful token refresh.
- Respect `Retry-After`.
- Persist `cooldownUntil` after HTTP 429 or Proton anti-abuse/rate-limit codes.
- During cooldown, fail fast with `rate_limited` and the remaining wait time.
- Redact all secrets and auth tokens in logs.
- Default test commands must never hit Proton network endpoints.

## Implementation Phases

### Phase 0: Documentation and preflights

- Keep `proton-sdk-bridge.md`, `live-canary-runbook.md`, and the formal
  requirements matrix as the source of truth for auth changes.
- Add a preflight that detects stale SDK `dist` vs `src` before bridge tests.
- Replace desktop app-version strings with
  `external-drive-proton-lfs-cli@<version>`.

### Phase 1: Auth API parity

Superseded. Do not add direct SRP account-login parity to the transfer bridge.
Account authorization follows the browser-fork path exposed by proton-drive-cli.

### Phase 2: Session manager consolidation

- Move all refresh behavior into one locked `SessionManager.refresh()`.
- Make proactive and reactive refresh use the same method.
- Persist `tokenExpiresAt` uniformly for login and refresh.
- Store and check `refreshGeneration`.
- Add cross-process refresh race tests.

### Phase 3: Transfer auth gate

- Keep bridge transfer request schemas free of account-login fields.
- Treat key-password/data-password failure as typed auth states, not as a
  reason to login.
- Add structured bridge errors for every auth state.

### Phase 4: Credential providers

- Keep pass-cli lookup scoped to key-password/data-password provider entries.
- Search all vaults for matching provider hosts where pass-cli supports it.
- Avoid environment variables for long-lived secrets in normal usage.

### Phase 5: Offline verification

Build a local fake Proton API that covers:

- one-password successful login
- two-password successful login
- TOTP required and completed
- FIDO2 required and stopped
- human verification required and stopped
- wrong login password
- wrong data password
- expired access token followed by refresh rotation
- consumed refresh token race between subprocesses
- invalid refresh token deauthorization
- HTTP 429 and `Retry-After`
- Proton rate-limit/anti-abuse error payloads

Tests must assert:

- exact endpoint sequence
- exact headers, including app version and UID
- no retry after wrong credentials
- no retry after rate limit or human verification
- no full login after key unlock failure
- no secrets in logs

### Phase 6: Live canary gate

Only after all offline tests pass:

1. Use a disposable Proton account with Drive already initialized in the web UI.
2. Run exactly one login attempt with verbose but redacted logs.
3. Verify session persistence and one small Drive metadata read.
4. Do not run upload/download load tests in the same session.
5. If any rate-limit, human-verification, or auth anomaly appears, stop and
   return to offline fixtures.

Operational procedure: `docs/operations/live-canary-runbook.md`.

## Acceptance Criteria

Auth is considered ready for a real canary only when:

- `make test` passes.
- Proton-drive-cli auth unit tests pass.
- The fake Proton API suite passes.
- Concurrent refresh race tests pass.
- Session JSON schema migration tests pass.
- SDK build freshness preflight passes.
- A dry-run bridge command can explain exactly which auth state it is in
  without touching Proton network endpoints.

## Non-Goals

- Implementing FIDO2 in the non-interactive bridge path.
- Persisting raw passwords or TOTP codes.
- Replacing the official Drive SDK.
- Using real Proton accounts in CI.
