# Tray UX and Session Refresh Plan

## Goals

- Make the tray a status surface first, not a launcher menu.
- Never show `Connected` when transfers are blocked by local prerequisites.
- Keep provider choices visible and flat; avoid nested menus for routine
  choices.
- Make browser-fork progress and refresh-token maintenance debuggable from
  logs without exposing tokens or session JSON.

## Menu Order

1. `Proton LFS <version>` disabled title.
2. `Status: <Ready | Setup needed | Not connected | Checking...>`.
3. `Session: <Signed in | Not connected | Expired | Invalid>`.
4. `Transfers: <Ready | Data password needed | Sign-in required | ...>`.
5. `Refresh: <Due in ... | Updating... | Failed; retry in ... | Reconnect required>`.
6. Primary auth action:
   - `Connect to Proton...` when no session exists.
   - `Disconnect from Proton` when a saved session exists.
7. `Configure Data Password...` only enabled for a data-password blocker.
8. `Recheck Status`.
9. Provider choices as top-level check items:
   - `Use Proton Pass`.
   - `Use Git Credential Manager`.
10. `Enable LFS Backend` / `LFS Backend Enabled`.
11. `Open Logs`.
12. `Run Doctor...`.
13. `Start at System Login`.
14. `Quit Proton LFS`.

## State Rules

- `Ready` means a saved browser-fork session exists, the local access token is
  not expired, browser-fork key material is recorded, and no local transfer
  blocker is known.
- `Setup needed` means the user is signed in but transfers are blocked. The
  common cases are missing browser-fork key material, an expired local token,
  an invalid session shape, or a legacy unlock path without an explicit data
  credential.
- `Not connected` means there is no saved Proton Drive session.
- `Connected` is not a menu state. The app uses `Signed in` for session state
  and `Ready` for transfer readiness.

## Refresh Behavior

- Refresh is token maintenance only. It must never trigger account-password
  login, SRP login, browser-fork login, or a transfer.
- The tray schedules refresh from `tokenExpiresAt`, refreshing near expiry
  instead of rotating a fresh token every few minutes.
- Refresh success records a visible last-refresh state.
- Recoverable refresh failure records a visible retry state and logs the
  redacted failure.
- A revoked, invalid, or expired refresh token must surface as action required;
  it must not loop into automatic login or repeat refresh attempts for the same
  saved session.
- The tray calls `proton-drive-cli session refresh --json`, consumes the
  redacted `statusCode`, Proton `Code`, `errorCode`, and `recoverable` fields,
  and treats non-recoverable failures such as Proton `10013` as reconnect
  required until browser login creates a new session.

## Debuggability

- Connect runs with auth trace environment variables.
- Tray subprocess output is streamed to `~/.proton-lfs/tray.log`.
- Logs redact bearer tokens, access/refresh/session tokens, UIDs, and
  password-like assignments before writing.
- `Open Logs` tails the same log file used by the tray logger.
- `Run Doctor...` opens the offline doctor with the selected key-password
  provider and any configured data-password provider.

## Design References

- Apple menu bar guidance: keep menu bar items predictable and directly
  actionable.
- Microsoft notification-area guidance: tray icons should communicate status
  without becoming noisy primary launchers.
- Nielsen Norman Group menu guidance: keep menu labels clear, consistently
  placed, and easy to scan.
