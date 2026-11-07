# Tray Reference

`proton-lfs-tray` is the system tray application and, when started with an
argument, a small command-line helper. `make install` and the release bundles
ship it as `proton-lfs-tray`; `make install` also links it as
`proton-lfs-cli`, the name its own usage text uses. Source of truth:
`cmd/tray/main.go` (usage, subcommands, single instance), `menu.go`,
`status.go`, `connect.go`, `cli.go`, `scope_diagnostics.go`, `setup.go`,
`gitconfig.go`, `custom_transfer_args.go`, `traylog.go`.

## Subcommands

| Invocation | Does | Exit status |
| --- | --- | --- |
| `proton-lfs-tray` | Start the tray. A second instance finds the lock held, prints `proton-lfs-cli is already running` and exits | 0 |
| `proton-lfs-tray login` | Run `proton-drive-cli login --credential-store <store>` with the store from `config.json` and `PROTON_PASS_CLI_BIN` set as in [Connect](#connect-to-proton); the CLI's stderr is shown on failure | 0 on success, 1 on failure or when proton-drive-cli is not found |
| `proton-lfs-tray logout` | Run `proton-drive-cli logout` (5-minute timeout), which revokes the session, deletes the session file and crypto cache and then removes the credential-store entry | 0, 1 |
| `proton-lfs-tray register` | Write the global git LFS registration (see below) and print the adapter path, proton-drive-cli path and credential store | 0, 1 when the adapter binary is not found or git fails |
| `proton-lfs-tray status` | Print `Session:` (`signed in; credential store <store> (<host>); proton-drive-cli doctor verifies it`, `invalid session file; run proton-drive-cli login` or `not connected`), `LFS:`, `Store:` (the credential store of the next login), `Transfer:`, `Readiness:` and `Refresh:` lines | 0 |
| `proton-lfs-tray scope-diagnostics [--live]` | Print the redacted JSON diagnostics report (see below) | 0, 1, 2, 3 |
| `proton-lfs-tray config [store]` | Print the credential store, or set it to `pass-cli` or `git-credential` | 0, 1 on an unknown store or a write error |
| `proton-lfs-tray --version` | Print `proton-lfs-cli <version>` | 0 |
| `proton-lfs-tray --help` | Print the usage text | 0 |
| any other word | Print `unknown command` and the usage text | 1 |

Each subcommand accepts `--help`. On macOS and Linux the tray and every
subcommand except `config` first adopt the `PATH` of the login shell
(`$SHELL -lc`, zsh when `SHELL` is unset, 5-second limit) so that `pass-cli`
and `node` installed by Homebrew, nvm or into `~/.local/bin` are found when
the tray starts from a `.app` bundle, a LaunchAgent or an XDG autostart
entry. Every invocation, subcommands included, writes its log lines to the
tray log as well as to stderr.

## Menu

Items in order, from `menu.go`:

1. `Proton LFS <version>` (disabled title).
2. `Status: <Ready | Setup needed | Not connected | Checking...>`.
3. `Session: <Signed in (<store>)[, token refresh due] | Not connected | Invalid | Refresh failed | Checking...>`, where `<store>` is the credential store recorded in the session.
4. `Transfers: <Ready | Sign-in required | Reconnect required | Checking...>`.
5. `Refresh: <Due in … | Due now | Updating... | Last refreshed … | Failed; retry in … | Failed | Reconnect required | No session | Checking...>`.
6. `Connect to Proton...` while no session file can be read as JSON, `Disconnect from Proton` otherwise.
7. `Recheck Status`: re-reads everything and runs a refresh when one is due.
8. `Use Proton Pass` and `Use Git Credential Helper` (check items; exactly one is checked; the choice is saved to `config.json` and applies to the next Connect).
9. `Enable LFS Backend` / `LFS Backend Enabled` (check item; writes the git registration).
10. `Open Logs`: opens a terminal running `tail -n 200 -f ~/.proton-lfs/tray.log`.
11. `Run Doctor...`: opens a terminal running `proton-drive-cli doctor` without credential store options, so the doctor verifies the credential store and host recorded in the session; the window stays open until Enter is pressed.
12. `Start at System Login` (check item).
13. `Quit Proton LFS`.

The four readout lines are disabled items. `Connected` is not a state: the
menu uses `Signed in` for the session and `Ready` for transfer readiness, so
a signed-in user whose transfers are blocked sees `Setup needed`.

### Readiness rules

The readiness shown in the four readout lines is derived every 5 seconds
from `~/.proton-drive-cli/session.json` and the refresh state
(`inspectAuthReadiness` in `status.go`):

| Condition (first match wins) | Status | Session | Transfers | Icon |
| --- | --- | --- | --- | --- |
| No session file | Not connected | Not connected | Sign-in required | idle |
| Session file that does not decode as a session, or lacks `uid`, `credentialStore`, `credentialHost` or `revision`, has a blank one, or names a store other than `pass-cli` or `git-credential` (proton-drive-cli's own session check) | Setup needed | Invalid | Reconnect required | error |
| A non-recoverable refresh failure was recorded for this `uid` | Setup needed | Refresh failed | Reconnect required | error |
| otherwise | Ready | Signed in (`credentialStore`), with `token refresh due` when `tokenExpiresAt` is in the past; bridge commands refresh an expired access token before use | Ready | ok |

Readiness comes from `session.json` alone. The session refresh, logout and
scope diagnostics the tray starts read the session entry through
proton-drive-cli, with `PROTON_PASS_CLI_BIN` set as in
[Connect](#connect-to-proton). A session entry the store cannot return is
reported by a failed refresh, by `bridge auth-state` (`credentials_unavailable`)
when a transfer session starts; an entry that cannot be read or whose key
password does not unlock the keys is reported by the transfer itself
(`credentials_unavailable`) and by `Run Doctor...`. The `LFS Backend
Enabled` check mark comes from the global git config (see
[Register](#register-enable-lfs-backend)).

### Icon and tooltip

The icon set has four images: idle (grey), ok (green), error (red) and
syncing (blue). While the user is not signed in, readiness is blocked, or the
status file is missing or reports `ok`, or reports `auth_required`, `captcha`
or a login `rate_limited` older than a login seen while the tray runs (a new
session `uid`), the readiness decides
the icon and tooltip. Otherwise the adapter's last transfer state decides:
`transferring` gives syncing, `error`, `rate_limited`, `auth_required` and
`captcha` give the error icon with a tooltip naming the state, and the
tooltip appends `(retryable, temporary)` when the status file says so. See
[Status File Reference](status-file.md).

## Connect to Proton

1. If the status file records a rate-limited login (`state: rate_limited`,
   `lastOp: login`, `errorCode: RATE_LIMITED`) less than one hour old, the
   click is refused with the notification `Login rate-limited; wait before retrying`.
2. `proton-drive-cli login --credential-store <store> -q` is run with
   `PROTON_AUTH_TRACE=1` and `PROTON_AUTH_TRACE_ID=tray-<pid>-<nanoseconds>`;
   unless the environment sets `PROTON_PASS_CLI_BIN`, it is set to the
   pass-cli found on `PATH`, `~/.local/bin`, `/opt/homebrew/bin` or
   `/usr/local/bin`, as for every proton-drive-cli command the tray starts.
3. Output is streamed line by line to the tray log. Bearer tokens, values of
   secret-named keys (`access…`, `refresh…`, `session…`, `password`,
   `secret`, `token`, `uid`) in JSON, JavaScript-object and `key=value` form,
   and `AccessToken <value>`-style words are redacted.
4. On success the refresh state is cleared and one of `Proton LFS Ready`,
   `Signed in; setup needed` or `Signed in` is shown. When the output carries
   `RATE_LIMITED` or Proton code 2028, the rate-limit status is written and
   `Login rate-limited by Proton` is shown; other failures show `Login failed`.

The browser fork delivers the key password for every account type;
two-factor authentication and CAPTCHA are completed in the browser.
proton-drive-cli stores it with the session tokens in the credential store.
A login that yields a new session UID removes the credential-store entry of
the previous UID.

`Disconnect from Proton` runs `proton-drive-cli logout` (5-minute timeout),
which revokes the session, deletes the session file and crypto cache and
then removes the credential-store entry; the tray then clears its refresh state and
shows `Disconnected from Proton` (`Disconnect failed` on an error).

## Session refresh

Refresh is token maintenance only; it never logs in and never transfers.

- On every 5-second poll and on `Recheck Status` the tray reads
  `tokenExpiresAt` from the session file and runs
  `proton-drive-cli session refresh --json` once the time is 10 minutes,
  or half the token lifetime when that is shorter, before expiry or later.
  A session without `tokenExpiresAt` (`ExpiresIn` is optional in Proton's
  auth responses) is refreshed 12 hours after `session.json` was last
  written; login and every refresh rewrite it (and the credential-store
  entry). No attempt follows a success
  within 1 minute, and an invalid session is never refreshed.
- `session refresh --json` reporting `no_session` is a non-recoverable
  failure for that `uid`, not a success.
- A recorded failure is dropped once `session.json` is written after it,
  by a login or by another process's refresh.
- Each run is bounded to 5 minutes, longer than proton-drive-cli's session
  lock wait plus a credential-store save. A timeout, a non-JSON failure, or a JSON
  result with `recoverable: true` (or without `recoverable`) is retried with
  binary exponential backoff, 1, 2, 4, ... minutes up to 30, or after its
  `retryAfter` seconds when that is later, and shown as
  `Refresh: Failed; retry in <n>m`. proton-drive-cli reports
  `recoverable: false` only when the session ended. A failure recorded for
  another session `uid` is ignored.
- A result with `recoverable: false` is remembered for that session `uid`
  and no further refresh is attempted for it. When proton-drive-cli rejected
  the refresh token (a Proton error body with HTTP 4xx other than 408 and
  429, while no other process had rotated it), it has signed the session out: `session.json`, the crypto
  cache and the credential-store entry are deleted and the menu shows
  `Not connected`. While the session file remains, the menu shows
  `Session: Refresh failed`, `Transfers: Reconnect required` and
  `Refresh: Reconnect required`.
- A successful Connect clears the refresh state.
- A refresh result's `statusCode`, `protonCode`, `errorCode` and `action` are
  logged; token values are not.

## Register (Enable LFS Backend)

Both the menu item and `proton-lfs-tray register` write, with
`git config --global`:

| Key | Value |
| --- | --- |
| `lfs.customtransfer.proton.path` | the adapter found next to the tray binary (`git-lfs-proton-adapter`, or `../Helpers/git-lfs-proton-adapter` inside a macOS `.app`) |
| `lfs.customtransfer.proton.args` | `--backend sdk`, plus `--drive-cli-bin <path>` when proton-drive-cli is found next to the tray binary; an argument with characters outside `A-Za-z0-9_@/.-` is single-quoted for the POSIX shell git-lfs uses |
| `lfs.standalonetransferagent` | `proton` |

The credential store is not part of the arguments: the adapter never
logs in. The `LFS Backend Enabled` check mark reflects
`lfs.standalonetransferagent = proton`; the global config files are re-read
only when their size or mtime changes.

## scope-diagnostics

`proton-lfs-tray scope-diagnostics` prints one JSON document with
`generatedAt`, `ok`, `live`, `summary`, `safety`, `config`
(`driveCliFound`, `driveCliPath`, `credentialStore` from `config.json`,
`storageBase`, the app-version inputs `adapterRequestAppVersion`,
`adapterRequestAppVersionSource`, `driveCliEnvAppVersion` and
`expectedDriveCliDefaultAppVersion`, and `driveApiBase`), `session`
(`present`, `path`, `fileMode`, `uidFingerprint` (the first 12 hex digits of
the UID's SHA-256), sorted `scopes`, `tokenExpiresAt`, `credentialStore`,
`credentialHost`; `rawTokenValuesIncluded` is always false),
`commandsAttempted`, `authState` (the redacted `bridge auth-state` run),
with `--live` `liveProbe`, and `errors`. The bridge runs get the adapter's
environment allowlist. In their requests, payloads and details, non-boolean
values of sensitive keys are replaced by `[redacted]` by the rules of
proton-drive-cli's `redactSensitive` (`internal/bridgeproto`); free text has
bearer tokens, secret-named fields and URL userinfo masked.

| Exit status | Meaning |
| --- | --- |
| 0 | Local diagnostics completed; with `--live`, the probe passed |
| 1 | An unknown option, proton-drive-cli not found, `auth-state` failed, or the live probe failed for a reason other than scope |
| 2 | `--live` refused: `authState.state` is not `ready`, or `PROTON_LFS_LIVE_CANARY` is not the exact acknowledgement `I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT` |
| 3 | The live probe hard-stopped on `INSUFFICIENT_SCOPE` / Proton code 9101 |

The live probe is exactly one `bridge list` request with `folder: "/"`
through `https://drive-api.proton.me`; it never runs login, init, upload,
download, delete or refresh, and the report contains only the file count of
the listing. The bridge runs with the adapter's environment allowlist plus
`PROTON_PASS_CLI_BIN`, bounded by the 5-minute subprocess timeout.

## Files

| Path | Role |
| --- | --- |
| `~/.proton-lfs/status.json` | Read every 5 seconds; written by the tray only for a rate-limited login |
| `~/.proton-lfs/config.json` | Preferences, see [Config File Reference](config-file.md) |
| `~/.proton-lfs/tray.log` | Append-only log (`0600`) of every tray action, notification and redacted subprocess output; also written to stderr |
| `~/.proton-lfs/tray.lock` | Advisory single-instance lock (flock / LockFileEx), released by the kernel when the process exits |
| `~/.proton-drive-cli/session.json` | Read for readiness and refresh scheduling; never written by the tray |
| `~/Library/LaunchAgents/com.proton.lfs-tray.plist` | macOS autostart entry (`RunAtLoad`, no `KeepAlive`, `ProcessType` `Interactive`); the label equals the `.app` bundle's `CFBundleIdentifier` |
| `$XDG_CONFIG_HOME/autostart/proton-lfs.desktop` | Linux autostart entry (`~/.config` when `XDG_CONFIG_HOME` is unset) |

Binary discovery looks for `git-lfs-proton-adapter` and `proton-drive-cli`
(with `.exe` on Windows) in the tray executable's directory and, on macOS,
in `../Helpers/`.

## Platform notes

- Notifications: `osascript` banner on macOS, `notify-send` on Linux; every
  notification is also a log line, which is the only channel on Windows.
- `Open Logs` and `Run Doctor...` open Terminal.app on macOS and the first of
  `x-terminal-emulator`, `gnome-terminal` and `xterm` found on Linux; on
  Windows, or when no terminal starts, they fall back to a notification
  (`Logs: <path>`, `Doctor could not open terminal`) because the helper
  snippets are POSIX shell.
- `Start at System Login` is implemented for macOS and Linux; on Windows the
  toggle fails and stays unchanged.
- `make install` installs the tray on macOS (`/Applications/ProtonLFS.app`)
  and Linux (`~/.local/bin`); on Windows the release `.zip` is unpacked by
  hand.

## Design norms

- The tray is a status surface first: readiness lines come before actions.
- Credential store choices are flat check items, not a submenu.
- Browser-fork progress and refresh maintenance are debuggable from the log
  without exposing tokens or session JSON.
- Menu bar guidance followed: Apple Human Interface Guidelines (menu bar
  extras are predictable and directly actionable), Microsoft notification
  area guidance (icons communicate status, not launch menus), Nielsen Norman
  Group menu guidance (clear, consistently placed, scannable labels).
