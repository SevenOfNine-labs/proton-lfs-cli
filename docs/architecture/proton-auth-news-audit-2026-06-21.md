# Proton Auth News Audit - 2026-06-21

This note records the latest Proton auth and SDK signals reviewed before any
real Proton login canary. No live login or transfer was attempted during this
audit.

## Sources Reviewed

- Proton blog: `https://proton.me/blog/drive-sdk-june-2026`
- Proton blog: `https://proton.me/blog/drive-cryptography-update`
- Proton blog: `https://proton.me/blog/proton-drive-cli`
- Proton blog: `https://proton.me/blog/authenticator-app`
- Proton support: `https://proton.me/support/switch-two-password-mode`
- Proton support: `https://proton.me/support/two-factor-authentication-2fa`
- Official SDK upstream: `https://github.com/ProtonDriveApps/sdk`
- Proton Pass CLI upstream: `https://github.com/protonpass/pass-cli`

## Findings

### Drive SDK and Official CLI

- Proton's June 2026 Drive SDK messaging emphasizes SDK-based Drive operations,
  not SDK-owned account authentication.
- Proton's June 2026 Drive cryptography update reinforces that SDK freshness is
  operationally important: clients that do not support the active Drive crypto
  model can be unable to update newer files. This does not move login/session
  ownership into the SDK, but it raises the cost of stale SDK pins before any
  live canary.
- The official SDK README at `ProtonDriveApps/sdk@f21e74cc` states that the SDK
  does not include authentication/login flows, session management, or user
  address providers. Integrating applications must still own that layer.
- The same README says third-party clients must identify honestly with
  `x-pm-appversion`, using the `external-drive-{name}@{semver}` family of
  values. Clients that spoof official Proton clients or ship unsafe behavior
  may be limited or blocked.
- Official SDK commit `24a1895f` changed browser-fork authentication so the
  official CLI uses `cli-drive`, while third-party/forked app versions use
  `external-drive`. We should keep `external-drive-proton-lfs-cli@...` and not
  borrow the official CLI client identity.
- Official SDK commit `823b724d` improved auth UX for browser-fork login by
  clearer terminal output and JSON sign-in URL support.

### Proton Account Authentication

- Proton still supports two-password mode, but the current support page calls
  it a legacy feature and recommends one-password mode plus modern 2FA for most
  users.
- Two-password mode still means the login password verifies identity, while the
  second mailbox/data password decrypts account data. Our separate
  `dataPassword` / `dataCredentialProvider` path remains necessary.
- Proton's 2FA support continues to allow authenticator-app TOTP and U2F/FIDO2
  security keys. Our current TOTP path is useful, and FIDO2-only accounts should
  continue to stop with explicit guidance instead of retrying SRP.
- Proton Authenticator reinforces that TOTP codes are generated locally and
  expire quickly. That supports our existing "operator supplies one code for one
  login attempt" model.

### Proton Pass CLI

- `pass-cli` moved from our pinned `1.4.3` snapshot to `2.1.4`.
- Security/auth-adjacent changelog items include:
  - `2.0.0`: migration to a newer networking/session-management stack.
  - `2.0.1`: improved session persistence.
  - `2.1.1`: checks for new user keys.
  - `2.1.3`: missing signature verification fix and more resilient SSH agent.
  - `2.1.4`: dotenv secret-reference masking and secret-reference path
    disambiguation.

## Local Updates

- Updated `submodules/pass-cli` to `e131e37` (`2.1.4`).
- Updated `submodules/proton-drive-cli/submodules/sdk` to the official SDK
  upstream `f21e74cc` (`cli/v0.4.6`, latest reachable `js/v0.17.3` lineage).
- Updated the nested SDK submodule URL to
  `https://github.com/ProtonDriveApps/sdk.git`.
- Added `@protontech/crypto@^2.0.0` to `proton-drive-cli` because the updated
  SDK declares it as a peer dependency.

## Compatibility Notes

- The official SDK still contains a gitlink at `kt/sdk/src/main/jniLibs`
  without a `.gitmodules` mapping. Fully recursive submodule commands can fail
  when they descend into that upstream path.
- Current mocked auth and bridge tests pass against the updated SDK snapshot.
- The updated SDK does not replace our local auth/session orchestration. It
  instead makes the boundary clearer: SDK for Drive crypto/business logic,
  local code for login, 2FA handling, data password handling, session refresh,
  and offline safety gates.

## Next Auth Hardening Implications

- Keep the live-login ban-avoidance rule: no real Proton login until offline
  gates and runbook checks pass.
- Keep the third-party app-version identity:
  `external-drive-proton-lfs-cli@...`.
- Consider a future optional browser-fork login mode modeled on the official
  CLI. This may be less brittle than direct SRP for first login, but it still
  must be gated behind the same offline and live-canary controls.
- Keep the two-password/data-password split. Latest Proton guidance still
  confirms the second password is data-decryption material, not a login secret.
