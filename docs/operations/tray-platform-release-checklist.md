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
- Simulate `session_expired` and confirm it does not attempt full login.
- Simulate `session_invalid` and confirm transfer actions are blocked.
- Trigger a mocked refresh success and confirm the visible state returns to
  ready without exposing tokens or raw session JSON.
- Trigger a mocked refresh failure and confirm the visible message includes a
  recovery action, not a retry loop.

## Release Evidence

Record:

- OS and desktop environment.
- Adapter commit and `proton-drive-cli` submodule commit.
- `make check-submodules` output.
- Credential provider used.
- Whether two-password/data-password mode was tested.
- Screenshot or terminal output with secrets redacted.
