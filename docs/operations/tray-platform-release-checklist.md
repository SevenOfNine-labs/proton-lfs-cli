# Tray Platform Release Checklist

Use this checklist before publishing a release that includes tray/helper
changes, status reporting changes, credential-store changes, or session
refresh behavior changes.

## Status Watcher

- Start the tray/helper with a clean `~/.proton-lfs/status.json`.
- Verify the readiness icon appears before any transfer.
- Run mocked upload and download flows and confirm the adapter writes `ok`
  per completed object and nothing for `init` and `terminate`, so a failed
  transfer stays visible after its session ends.
- Force a mocked auth blocker and confirm the tray/helper reports the machine
  error code rather than a free-form string only.
- Confirm status updates recover after deleting and recreating the status file.

## Autostart

- Verify install and uninstall paths on each supported desktop platform.
- Confirm duplicate autostart entries are not created on repeated install.
- Confirm uninstall removes only entries owned by this project.
- Confirm manual launch still works when autostart is disabled.

## Credential Store Selection

- Verify `pass-cli` is the default credential store (`credentialStore` in
  `~/.proton-lfs/config.json`).
- Verify `git-credential` can be selected through the tray check item and
  `proton-lfs-tray config git-credential`, and that `register` writes no
  credential store flag into `lfs.customtransfer.proton.args`.
- Verify store names shown in tray/helper UI match the bridge contract:
  `pass-cli` or `git-credential`.
- With `git-credential`, verify login fails with a message naming the allowed
  helpers when only `store` or `cache` is configured.

## Session Refresh Display

- Start with a ready local session and confirm the tray/helper shows ready.
- Start with a session file that lacks `credentialStore`,
  `credentialHost` or `revision`; confirm the tray shows `Setup needed` /
  `Session: Invalid`, not connected/ready.
- Remove the session's entry from its credential store; confirm
  `Run Doctor...` reports the `session-credentials` check as failed and a
  push fails at `init` with `Proton session credentials are unavailable
  from the credential store`.
- Simulate `session_expired` and confirm the transfer refreshes the token at
  `init` and does not attempt a login.
- Simulate `session_invalid` and confirm transfer actions are blocked.
- Trigger a mocked refresh success and confirm the visible state returns to
  ready without exposing tokens or raw session JSON.
- Trigger a mocked refresh failure and confirm the visible message includes a
  recovery action, not a retry loop.

## Canary Gates

- Run `make live-canary-preflight` with no live doctor args and confirm it
  skips credential-store doctor instead of touching Proton.
- Run the browser-fork canary script tests with `go test ./scripts` and confirm
  fake-drive coverage still proves one login command and no transfer commands.
- Confirm `make test-e2e-real` is not part of default CI or `make test`.
- For any real disposable-account run, record the exact
  `LIVE_CANARY_DOCTOR_ARGS` and acknowledgement used.

## Transfer Robustness

- Run `go test ./cmd/adapter` and confirm fail-closed upload dedup coverage is
  present for non-404 `exists` failures.
- Confirm status output surfaces retryable/temporary metadata for transient
  transfer failures.
- Confirm large-object progress coverage uses virtual or mocked data, not
  checked-in large fixtures.
- Confirm `batch-exists` and `batch-delete` are rejected as Git LFS transfer
  events.

## Release Evidence

Record:

- OS and desktop environment.
- Adapter commit and `proton-drive-cli` submodule commit.
- `make check-submodules` output.
- `make live-canary-preflight` output.
- `go test ./cmd/adapter ./cmd/tray ./internal/config ./internal/preflight ./scripts` output.
- Credential store used.
- Whether browser-fork canary was skipped, mocked only, or run with a
  disposable account.
- Screenshot or terminal output with secrets redacted.
