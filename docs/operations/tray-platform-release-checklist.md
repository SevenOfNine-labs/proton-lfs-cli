# Tray Platform Release Checklist

Use this checklist before publishing a release that includes tray/helper
changes, status reporting changes, credential-provider changes, or session
refresh behavior changes.

## Status Watcher

- Start the tray/helper with a clean `~/.proton-lfs/status.json`.
- Verify the idle state appears before any transfer.
- Run mocked upload and download flows and confirm status transitions:
  `idle` -> transfer activity -> `ok`.
- Force a mocked auth blocker and confirm the tray/helper reports the machine
  error code rather than a free-form string only.
- Confirm status updates recover after deleting and recreating the status file.

## Autostart

- Verify install and uninstall paths on each supported desktop platform.
- Confirm duplicate autostart entries are not created on repeated install.
- Confirm uninstall removes only entries owned by this project.
- Confirm manual launch still works when autostart is disabled.

## Credential Provider Selection

- Verify `pass-cli` remains the default provider for adapter transfers.
- Verify `git-credential` can be selected through adapter args or environment.
- Verify two-password/data-password provider fields are displayed distinctly
  from login credential provider fields.
- Verify provider names shown in tray/helper UI match the bridge contract:
  `pass-cli` or `git-credential`.

## Session Refresh Display

- Start with a ready local session and confirm the tray/helper shows ready.
- Start with a two-password session and no data credential provider; confirm
  the tray/helper shows setup-needed, not connected/ready.
- Simulate `session_expired` and confirm it does not attempt full login.
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
- Confirm `batch-exists` and `batch-delete` remain documented as private
  maintenance helpers and are rejected as Git LFS transfer events.

## Release Evidence

Record:

- OS and desktop environment.
- Adapter commit and `proton-drive-cli` submodule commit.
- `make check-submodules` output.
- `make live-canary-preflight` output.
- `go test ./cmd/adapter ./cmd/tray ./internal/config ./internal/preflight ./scripts` output.
- Credential provider used.
- Whether two-password/data-password mode was tested.
- Whether browser-fork canary was skipped, mocked only, or run with a
  disposable account.
- Screenshot or terminal output with secrets redacted.
