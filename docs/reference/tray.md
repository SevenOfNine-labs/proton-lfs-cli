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
| `proton-lfs-tray login` | Run `proton-drive-cli login --key-password-provider <provider>` with the provider from `config.json`; the CLI's stderr is shown on failure | 0 on success, 1 on failure or when proton-drive-cli is not found |
| `proton-lfs-tray logout` | Run `proton-drive-cli logout` (2-minute timeout) | 0, 1 |
| `proton-lfs-tray register` | Write the global git LFS registration (see below) and print the adapter path, proton-drive-cli path and provider | 0, 1 when the adapter binary is not found or git fails |
| `proton-lfs-tray status` | Print `Session:`, `LFS:`, `Provider:`, `Transfer:`, `Readiness:` and `Refresh:` lines | 0 |
| `proton-lfs-tray scope-diagnostics [--live]` | Print the redacted JSON diagnostics report (see below) | 0, 1, 2, 3 |
| `proton-lfs-tray config [provider]` | Print the key-password provider, or set it to `pass-cli` or `git-credential` | 0, 1 on an unknown provider or a write error |
| `proton-lfs-tray --version` | Print `proton-lfs-cli <version>` | 0 |
| `proton-lfs-tray --help` | Print the usage text | 0 |
| any other word | Print `unknown command` and the usage text | 1 |

Each subcommand accepts `--help`. On macOS and Linux the tray and every
subcommand except `config` first adopt the `PATH` of the login shell
(`$SHELL -lc`, zsh when `SHELL` is unset, 5-second limit) so that `pass-cli`
and `node` installed by Homebrew, nvm or into `~/.local/bin` are found when
the tray starts from a `.app` bundle, a LaunchAgent or an XDG autostart
entry.

On start the tray removes `--credential-provider <value>` from an existing
`lfs.customtransfer.proton.args` (written by releases before 2026-11; the
adapter no longer accepts it) and keeps every other argument.

## Menu

Items in order, from `menu.go`:

1. `Proton LFS <version>` (disabled title).
2. `Status: <Ready | Setup needed | Not connected | Checking...>`.
3. `Session: <Signed in | Not connected | Invalid | Expired | Refresh failed | Checking...>`.
4. `Transfers: <Ready | Sign-in required | Reconnect required | Refresh required | Data password needed | Checking...>`.
5. `Refresh: <Due in … | Due now | Updating... | Last refreshed … | Failed; retry in … | Failed | Reconnect required | No session | Scheduled | Checking...>`.
6. `Connect to Proton...` when no session file exists, `Disconnect from Proton` when one exists.
7. `Configure Data Password...`, enabled only while the blocker is a missing data password; opens a terminal with the setup steps.
8. `Recheck Status`: re-reads everything and runs a refresh when one is due.
9. `Use Proton Pass` and `Use Git Credential Manager` (check items; exactly one is checked; the choice is saved to `config.json`).
10. `Enable LFS Backend` / `LFS Backend Enabled` (check item; writes the git registration).
11. `Open Logs`: opens a terminal running `tail -n 200 -f ~/.proton-lfs/tray.log`.
12. `Run Doctor...`: opens a terminal running `proton-drive-cli doctor --key-password-provider <provider>` plus `--data-credential-provider`/`--data-credential-host` when configured.
13. `Start at System Login` (check item).
14. `Quit Proton LFS`.

The four readout lines are disabled items. `Connected` is not a state: the
menu uses `Signed in` for the session and `Ready` for transfer readiness, so
a signed-in user whose transfers are blocked sees `Setup needed`.

### Readiness rules

The readiness shown in the first four lines is derived every 5 seconds from
`~/.proton-drive-cli/session.json`, the refresh state and the global git
config:

| Condition (first match wins) | Status | Session | Transfers | Icon |
| --- | --- | --- | --- | --- |
| No session file | Not connected | Not connected | Sign-in required | idle |
| Session file lacks `sessionId`, `uid`, `accessToken`, `refreshToken`, `scopes` or a positive `passwordMode` | Setup needed | Invalid | Reconnect required | error |
| A non-recoverable refresh failure was recorded for this `sessionId` | Setup needed | Refresh failed | Reconnect required | error |
| `tokenExpiresAt` is in the past | Setup needed | Expired | Refresh required | error |
| `authMode` is `browser-fork` and `keyPasswordPersisted` is false | Setup needed | Signed in | Reconnect required | error |
| Not a browser-fork session with persisted key material and no data-credential provider configured (prefs, `PROTON_DATA_CREDENTIAL_PROVIDER` or `--data-credential-provider` in the git args) | Setup needed | Signed in | Data password needed | error |
| otherwise | Ready | Signed in | Ready | ok |

### Icon and tooltip

The icon set has four images: idle (grey), ok (green), error (red) and
syncing (blue). While the user is not signed in, readiness is blocked, or the
status file is missing or reports `idle`/`ok`, the readiness decides the icon
and tooltip. Otherwise the adapter's last transfer state decides:
`transferring` gives syncing, `error`, `rate_limited`, `auth_required` and
`captcha` give the error icon with a tooltip naming the state, and the
tooltip appends `(retryable, temporary)` when the status file says so. See
[Status File Reference](status-file.md).

## Connect to Proton

1. If the status file records a rate-limited login (`state: rate_limited`,
   `lastOp: login`, `errorCode: RATE_LIMITED`) less than one hour old, the
   click is refused with the notification `Login rate-limited; wait before retrying`.
2. `proton-drive-cli login --key-password-provider <provider> -q` is run with
   `PROTON_AUTH_TRACE=1` and `PROTON_AUTH_TRACE_ID=tray-<pid>-<nanoseconds>`;
   for `pass-cli`, `PROTON_PASS_CLI_BIN` is set to the binary found on `PATH`,
   `~/.local/bin`, `/opt/homebrew/bin` or `/usr/local/bin`.
3. Output is streamed line by line to the tray log. Bearer tokens, values of
   secret-named keys (`access…`, `refresh…`, `session…`, `password`,
   `secret`, `token`, `uid`) in JSON, JavaScript-object and `key=value` form,
   and `AccessToken <value>`-style words are redacted.
4. On success the refresh state is cleared and one of `Proton LFS Ready`,
   `Signed in; setup needed` or `Signed in` is shown. When the output carries
   `RATE_LIMITED` or Proton code 2028, the rate-limit status is written and
   `Login rate-limited by Proton` is shown; other failures show `Login failed`.

`Disconnect from Proton` runs `proton-drive-cli logout`, clears the refresh
state and shows `Disconnected from Proton`.

## Session refresh

Refresh is token maintenance only; it never logs in and never transfers.

- The tray reads `tokenExpiresAt` from the session file and schedules
  `proton-drive-cli session refresh --json` 10 minutes before expiry. Without
  an expiry it refreshes every 12 hours.
- Each run is bounded to 2 minutes. A timeout, a non-JSON failure, or a JSON
  result with `recoverable: true` is retried after 1 minute and shown as
  `Refresh: Failed; retry in <n>m`.
- A result with `recoverable: false` (for example Proton code `10013`,
  `Invalid refresh token`) is remembered for that `sessionId`; the menu shows
  `Refresh: Reconnect required` and `Transfers: Reconnect required`, and no
  further refresh is attempted until Connect creates a new session.
- `Recheck Status` and a successful Connect re-evaluate the schedule.
- A refresh result's `statusCode`, `protonCode`, `errorCode` and `action` are
  logged; token values are not.

## Register (Enable LFS Backend)

Both the menu item and `proton-lfs-tray register` write, with
`git config --global`:

| Key | Value |
| --- | --- |
| `lfs.customtransfer.proton.path` | the adapter found next to the tray binary (`git-lfs-proton-adapter`, or `../Helpers/git-lfs-proton-adapter` inside a macOS `.app`) |
| `lfs.customtransfer.proton.args` | `--backend sdk --drive-cli-bin <proton-drive-cli path>`, plus `--data-credential-provider <p> --data-credential-host <h>` when `config.json` names a data-credential provider; arguments are single-quoted for the POSIX shell git-lfs uses |
| `lfs.standalonetransferagent` | `proton` |

The key-password provider is not part of the arguments: the adapter never
logs in. The `LFS Backend Enabled` check mark reflects
`lfs.standalonetransferagent = proton`; the global config files are re-read
only when their size or mtime changes.

## scope-diagnostics

`proton-lfs-tray scope-diagnostics` prints one JSON document with
`generatedAt`, `ok`, `live`, `summary`, `safety`, `config`
(providers and their source, storage base, app-version inputs,
`driveApiBase`), `session` (file mode, SHA-256 fingerprints of UID and
session id, scopes, `passwordMode`, `authMode`, `keyPasswordPersisted`,
`tokenExpiresAt`, token presence flags; `rawTokenValuesIncluded` is always
false), `commandsAttempted`, `authState` (the redacted `bridge auth-state`
run) and, with `--live`, `liveProbe`. Keys containing `token`, `password`,
`secret`, `sessionid` or named `uid` are redacted to `[redacted]`.

| Exit status | Meaning |
| --- | --- |
| 0 | Local diagnostics completed; with `--live`, the probe passed |
| 1 | proton-drive-cli not found, `auth-state` failed, or the live probe failed for a reason other than scope |
| 2 | `--live` refused: `authState.state` is not `ready`, or `PROTON_LFS_LIVE_CANARY` is not the exact acknowledgement `I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT` |
| 3 | The live probe hard-stopped on `INSUFFICIENT_SCOPE` / Proton code 9101 |

The live probe is exactly one `bridge list` request with `folder: "/"`
through `https://drive-api.proton.me`; it never runs login, init, upload,
download, delete or refresh, and the report contains only the file count of
the listing. The bridge runs with the tray's full environment plus
`PROTON_PASS_CLI_BIN`, bounded by the 2-minute subprocess timeout.

## Files

| Path | Role |
| --- | --- |
| `~/.proton-lfs/status.json` | Read every 5 seconds; written by the tray only for a rate-limited login |
| `~/.proton-lfs/config.json` | Preferences, see [Config File Reference](config-file.md) |
| `~/.proton-lfs/tray.log` | Append-only log (`0600`) of every tray action, notification and redacted subprocess output; also written to stderr |
| `~/.proton-lfs/tray.lock` | Advisory single-instance lock (flock / LockFileEx), released by the kernel when the process exits |
| `~/.proton-drive-cli/session.json` | Read for readiness and refresh scheduling; never written by the tray |
| `~/Library/LaunchAgents/com.proton.lfs-tray.plist` | macOS autostart entry (`RunAtLoad`, no `KeepAlive`); a plist under the earlier label `com.proton.git-lfs-tray` counts as enabled and is removed whenever autostart is toggled |
| `$XDG_CONFIG_HOME/autostart/proton-lfs.desktop` | Linux autostart entry (`~/.config` when `XDG_CONFIG_HOME` is unset) |

Binary discovery looks for `git-lfs-proton-adapter` and `proton-drive-cli`
(with `.exe` on Windows) in the tray executable's directory and, on macOS,
in `../Helpers/`.

## Platform notes

- Notifications: `osascript` banner on macOS, `notify-send` on Linux; every
  notification is also a log line, which is the only channel on Windows.
- `Open Logs`, `Run Doctor...` and `Configure Data Password...` open a
  terminal emulator on macOS and Linux; on Windows they fall back to a
  notification because the helper snippets are POSIX shell.
- `Start at System Login` is implemented for macOS and Linux; on Windows the
  toggle fails and stays unchanged.
- `make install` installs the tray on macOS (`/Applications/ProtonLFS.app`)
  and Linux (`~/.local/bin`); on Windows the release `.zip` is unpacked by
  hand.

## Design norms

- The tray is a status surface first: readiness lines come before actions.
- Provider choices are flat check items, not a submenu.
- Browser-fork progress and refresh maintenance are debuggable from the log
  without exposing tokens or session JSON.
- Menu bar guidance followed: Apple Human Interface Guidelines (menu bar
  extras are predictable and directly actionable), Microsoft notification
  area guidance (icons communicate status, not launch menus), Nielsen Norman
  Group menu guidance (clear, consistently placed, scannable labels).
