# proton-lfs-cli Feature Audit

Last audited: 2026-11-06.

This document is the formal feature inventory for `proton-lfs-cli`. It covers
the Go Git LFS adapter, the tray/helper CLI, the bridge boundary to
`submodules/proton-drive-cli`, configuration, tests, and remaining gaps.
Runtime behavior and tests remain authoritative.

## Status Legend

| Status | Meaning |
| --- | --- |
| Stable | Implemented and covered by unit, protocol, or mocked integration tests. |
| Beta | Implemented and covered, but still needs broader opt-in real Proton validation. |
| Experimental | Implemented, but tied to changing upstream SDK/auth behavior. |
| Guarded | Requires explicit opt-in to avoid accidental real login or account risk. |
| Gap | Missing, incomplete, or intentionally not implemented. |

## Product Surfaces

| Surface | Purpose | Status | Primary code | Primary coverage |
| --- | --- | --- | --- | --- |
| Git LFS custom transfer adapter | Implements Git LFS standalone custom transfer protocol over stdin/stdout. | Stable for protocol/local backend; Beta for Proton SDK backend. | `cmd/adapter/main.go`, `cmd/adapter/backend.go`. | `cmd/adapter/*_test.go`, `tests/integration/git_lfs_custom_transfer*.go`. |
| Local backend | Deterministic filesystem object store for tests and offline development. | Stable | `cmd/adapter/backend.go`. | Adapter unit tests and local integration tests. |
| SDK backend | Spawns `proton-drive-cli bridge` subprocess and stores objects in Proton Drive under `/LFS`. | Beta | `cmd/adapter/bridge.go`, `cmd/adapter/backend.go`. | Bridge unit tests, mocked E2E, opt-in SDK integration. |
| Tray/helper CLI | `proton-lfs-tray` (linked as `proton-lfs-cli` by `make install`): login, logout, register, status, config, scope-diagnostics, and the tray itself. | Beta | `cmd/tray/*.go`. | `cmd/tray/*_test.go`. |
| Status reporting | Writes transfer state, machine error code, retryability, and temporary-failure metadata for tray/CLI display. | Stable | `internal/config/status.go`. | `internal/config/status_test.go`, tray status tests. |
| Preferences | Stores the key-password provider and the enabled flag. | Stable | `internal/config/prefs.go`. | `internal/config/config_test.go`, tray CLI tests. |
| Build/package automation | Builds Go adapter/tray, proton-drive-cli, Git LFS, SEA bundle, and app bundles. | Beta | `Makefile`, `scripts/*.sh`. | Exercised by the Build and Release Bundle workflows and by hand; not fully unit-tested. |
| Submodule pin verification | Verifies root submodule pins and the nested Proton SDK gitlink without recursive SDK traversal. | Stable | `scripts/check-submodules.sh`, `Makefile`. | `make check-submodules`. |
| Live canary workflow | Real Proton E2E with explicit acknowledgement. | Guarded | `Makefile`, `docs/operations/live-canary-runbook.md`. | Not run by default; requires `PROTON_LFS_LIVE_CANARY`. |

## Adapter Protocol Contract

The adapter is invoked by Git LFS, not directly by end users.

### Supported Git LFS Events

| Event/action | Behavior | Status | Tests |
| --- | --- | --- | --- |
| `init` | Validates operation, initializes selected backend, returns empty JSON object on success. | Stable | `cmd/adapter/protocol_spec_test.go`, `cmd/adapter/main_test.go`. |
| `upload` | Validates OID/path/size, sends progress, stores object, returns complete. | Stable local, Beta SDK. | Unit tests, local integration, mocked SDK E2E. |
| `download` | Validates OID, stages object to temp path, returns progress and complete with path. | Stable local, Beta SDK. | Unit tests, local integration, mocked SDK E2E. |
| `terminate` | Discards the session state and leaves the status file unchanged; the process ends when git-lfs closes stdin. | Stable | Protocol spec tests. |
| Unknown event | Returns protocol or transfer error without leaking secrets. | Stable | Unit tests. |
| `verify` action | Not implemented. | Gap | Documented as not required by current custom-transfer usage. |
| Resume/retry | Not implemented at adapter layer. | Gap | Retry remains Git LFS/outer workflow responsibility. |

### Adapter Flags and Environment

| Flag | Environment | Meaning | Status |
| --- | --- | --- | --- |
| `--backend` | `PROTON_LFS_BACKEND` | Select the local store (`local`) or the Proton SDK bridge backend (`sdk`). | Stable |
| `--local-store-dir` | `PROTON_LFS_LOCAL_STORE_DIR` | Local object store path. | Stable |
| `--drive-cli-bin` | `PROTON_DRIVE_CLI_BIN` | Path to `proton-drive-cli` entry point. | Stable |
| `--allow-mock-transfers` | `ADAPTER_ALLOW_MOCK_TRANSFERS` | Simulation mode for protocol tests only. | Guarded |
| `--bridge-timeout` | `PROTON_LFS_BRIDGE_TIMEOUT` | Base timeout per bridge command (default 5m; transfers add 1 s per 256 KiB). | Stable |
| `--debug` | none | Debug logging. | Stable |
| `--version` | none | Version output. | Stable |
| none | `NODE_BIN` | Node executable for drive-cli subprocess. | Stable |
| none | `LFS_STORAGE_BASE` | Remote LFS base folder. | Stable |
| none | `PROTON_APP_VERSION` | Proton API app-version header. | Stable |
| none | `PROTON_LFS_STATUS_FILE` | Override status JSON location. | Stable |

## SDK Bridge Boundary

The root adapter never resolves Proton credentials and never sends account
or credential fields to `proton-drive-cli` transfer commands. Account login
must already exist as a browser-fork session, whose key password the bridge
reads from the session's provider. The adapter sends only `storageBase`,
`appVersion` and operation fields.
proton-drive-cli owns the per-command request-field matrix in
`schemas/bridge/v1/request-field-rules.json`; root contract tests verify every
root bridge request shape remains allowed and includes newly required fields.

### Root-to-Drive-CLI Commands

| Root method | Bridge command | Request additions | Login allowed? | Status |
| --- | --- | --- | --- | --- |
| `AuthState` | `auth-state` | Storage base, app version. | No; local-only. | Stable |
| `InitLFSStorage` | `init` | Storage base, app version. | No | Beta |
| `Upload` | `upload` | `oid`, local `path`, storage base, app version. | No | Beta |
| `Download` | `download` | `oid`, `outputPath`, storage base, app version. | No | Beta |
| `Exists` | `exists` | `oid`, storage base, app version. | No | Beta; upload dedup fails closed on non-404 errors. |

The app version is sent only when `PROTON_APP_VERSION` is set.

### Auth-State Gate

`DriveCLIBackend.Initialize` must call `auth-state` before `init`. Transfers
proceed only when the state is `ready` or `session_expired`.

| Bridge state | Root classification | Transfer behavior |
| --- | --- | --- |
| `ready` | OK | Continue to `init`, upload/download. |
| `needs_login` | `auth_required` 401 | Refuse transfer; no session file; user must login outside Git LFS transfer. |
| `session_invalid` | `auth_required` 401 | Refuse transfer; the session file is unreadable or lacks a field `login` writes; replace it with `proton-drive-cli login`. |
| `session_expired` | OK | Continue to `init`, which refreshes the access token before use; a rejected refresh token fails `init` with `SESSION_EXPIRED` (`auth_required` 401). |
| `needs_key_password` | `key_password_required` 401 | Refuse transfer; the session's key-password entry is missing; rerun browser login. |
| Unknown state | `server_error` 502 | Refuse transfer; update compatibility. |

The bridge reports the first state that applies in the order `needs_login`,
`session_invalid`, `session_expired`, `needs_key_password`, `ready`. The refusal answers `init` with a protocol error; the
status file is not written.

## Tray and Helper CLI Surface

The tray binary also provides a small CLI when launched with arguments.

| Command/action | Behavior | Status | Tests |
| --- | --- | --- | --- |
| `proton-lfs-tray login` | Run browser-fork-only `proton-drive-cli login --key-password-provider <provider>`. | Beta | `cmd/tray/cli_test.go`. |
| `proton-lfs-tray logout` | Delegate to `proton-drive-cli logout`. | Beta | `cmd/tray/cli_test.go`. |
| `proton-lfs-tray register` | Configure global Git LFS custom transfer for Proton. | Stable | `cmd/tray/cli_test.go`, `custom_transfer_args_test.go`. |
| `proton-lfs-tray status` | Print session, LFS registration, provider, transfer status, readiness, and refresh state. | Stable | `cmd/tray/status_test.go`, `cmd/tray/cli_test.go`. |
| `proton-lfs-tray config [provider]` | Show or set preferred browser-fork key-password provider. | Stable | `cmd/tray/cli_test.go`. |
| `proton-lfs-tray scope-diagnostics [--live]` | Redacted auth/session evidence; `--live` performs one read-only `bridge list` behind the canary acknowledgement. | Beta | `cmd/tray/cli_test.go`. |
| Tray Connect | Run browser-fork-only login with the configured key-password provider. | Beta | `cmd/tray/connect_test.go`, `cmd/tray/cli_test.go`. |
| Tray status watcher | Poll transfer status and session/LFS registration; display retryability/temporary failure hints; schedule visible token refresh from saved token expiry. | Beta | `cmd/tray/status_test.go`. |
| Autostart | macOS LaunchAgent and Linux desktop autostart. | Beta | `cmd/tray/setup_test.go`; packaging/manual validation still needed. |

## Storage and Runtime Files

| File/location | Owner | Contents | Security expectation |
| --- | --- | --- | --- |
| `~/.proton-lfs/status.json` | Root adapter/tray | Last transfer state, OID, operation, error code/detail. | No secrets. |
| `~/.proton-lfs/config.json` | Tray/helper CLI | Key-password provider, enabled flag. | No secrets. |
| `~/.proton-drive-cli/session.json` | proton-drive-cli | Revocable Proton session tokens, scopes, key-password provider and host, token expiry. | Mode `0600`; no passwords. |
| Credential helper / Proton Pass | proton-drive-cli providers | The UID-scoped browser-fork key password. | Managed by provider, never root adapter config; removed on logout, on a login with a new UID and on a rejected refresh token. |
| Temp download files | Adapter | Staged objects returned to Git LFS. | Cleaned after transfer where possible; stale cleanup exists. |

## Test Coverage Matrix

| Feature area | Tests | Status |
| --- | --- | --- |
| Git LFS custom transfer protocol | `cmd/adapter/protocol_spec_test.go`, `cmd/adapter/main_test.go`. | Stable |
| Local backend upload/download/integrity | `cmd/adapter/backend_test.go`, `tests/integration/git_lfs_custom_transfer_test.go`. | Stable |
| Direction config matrix | `tests/integration/git_lfs_custom_transfer_config_matrix_test.go`. | Stable |
| Concurrency and stress | `tests/integration/git_lfs_custom_transfer_concurrency*.go`. | Stable local coverage |
| Adapter exit and failure modes | `tests/integration/git_lfs_custom_transfer_adapter_exit_test.go`, `git_lfs_custom_transfer_failure_modes_test.go`. | Stable local coverage |
| Bridge subprocess envelope/security | `cmd/adapter/bridge_test.go` covers strict envelopes, size-scaled timeouts, malformed output, stderr redaction, and environment filtering. | Stable |
| Bridge request-field contract | `cmd/adapter/bridge_contract_test.go` consumes drive-cli `request-field-rules.json` and fails when root request shapes drift. | Stable |
| Bridge success payload parsing | `cmd/adapter/bridge_test.go` rejects malformed `exists` and batch helper payloads instead of inferring success. | Stable |
| Auth-state mapping | `cmd/adapter/backend_test.go`, `cmd/adapter/bridge_test.go`. | Stable |
| Bridge request fields carry no credentials | `cmd/adapter/bridge_contract_test.go`, `cmd/adapter/bridge_test.go`, `tests/integration/credential_security_test.go`. | Stable |
| Mocked SDK E2E | `tests/integration/git_lfs_e2e_mock_test.go`, `tests/testdata/mock-proton-drive-cli.js`. | Stable and safe by default |
| Transfer robustness | `cmd/adapter/backend_test.go`, `cmd/adapter/main_test.go`. | Stable offline coverage for fail-closed dedup errors, progress semantics, and virtual multi-GiB copy counters. |
| Real SDK subprocess integration | `tests/integration/git_lfs_sdk_backend_test.go`. | Guarded; requires `PROTON_LFS_RUN_SDK_INTEGRATION=1` or `make test-integration-sdk`. |
| Real Proton E2E | `tests/integration/git_lfs_e2e_real_test.go`. | Guarded by `PROTON_LFS_LIVE_CANARY`. |
| Credential security | `tests/integration/credential_security_test.go`. | Partial; some checks skip without local session/pass-cli. |
| Tray/helper CLI | `cmd/tray/*_test.go`. | Stable for CLI logic; GUI/manual behavior remains Beta. |
| Shared config/status | `internal/config/*_test.go`. | Stable |
| Submodule verification | `make check-submodules`. | Stable for root-owned pins; intentionally skips recursive SDK descent. |
| Build/package scripts | `Makefile`, `scripts/*.sh`. | Partial; build targets are run manually/locally, scripts not fully unit-tested. |

## Formal Maturity Assessment

| Capability | Maturity | Reason |
| --- | --- | --- |
| Local Git LFS adapter | Stable | Full protocol and integration coverage without Proton dependency. |
| Proton SDK bridge transfer path | Beta | Mocked E2E and offline auth gates are strong; real Proton canary remains opt-in. |
| Browser-fork session handling across repos | Experimental to Beta | Key-password storage and transfer gating are covered; the real browser canary is pending. |
| Credential-provider delegation | Stable | Root never handles secrets; drive-cli provider contracts are tested. |
| Tray UX | Beta | Menu, readiness rules and refresh behaviour are documented in `docs/reference/tray.md`; visual/platform behavior still needs the manual release checklist. |
| Packaging/install | Beta | Scripts exist; cross-platform validation is manual. |

## Known Gaps and Required Follow-ups

| Gap | Risk | Required action |
| --- | --- | --- |
| No default real Proton transfer canary. | Mocked bridge can miss SDK/API changes. | Keep guarded; run with disposable account only after offline doctor and explicit acknowledgement. |
| Live canary doctor output drift. | A textual output change could accidentally bypass or block canary readiness. | Parse structured `doctor --json` readiness fields with the root preflight checker and bridge auth-state schema. |
| Docs link drift can recur as plans move to implemented docs. | New contributors can follow stale paths. | Keep `docs/README.md` and release checklists updated with each maturity change. |
| SDK adapter progress remains post-transfer. | Poor UX for large SDK-backed objects and timeouts. | Add SDK streaming progress when the drive-cli/SDK bridge exposes reliable callbacks. |
| Resume is not implemented. | Interrupted transfers restart after transient retry attempts are exhausted. | Retryable/temporary failures are surfaced in status JSON and helper/tray messaging; upload dedup fails closed on uncertain remote state; add resume only if SDK support is available. |
| Tray GUI/manual platform behavior lacks automation. | Menu/status/autostart regressions may escape unit tests. | Add release checklist and, if feasible, platform smoke automation. |
| Real SDK integration is opt-in but easy to confuse with mocked E2E. | Accidental auth attempts could create account risk. | Keep `PROTON_LFS_RUN_SDK_INTEGRATION` and `PROTON_LFS_LIVE_CANARY` gates; document them prominently. |
| Bridge contract drift remains possible when schemas change. | New states/errors/required request fields may be misclassified or omitted if tests are bypassed. | Keep drive-cli schemas and root contract tests required for every bridge change. |
| Official SDK layout or history can change between releases. | Future SDK updates can break drive-cli package paths or API assumptions before auth behavior is retested. | Keep SDK updates drive-cli-first, require lint/build/docs/full mocked tests there, then update the root submodule pointer and root checks. |

## Definition of Done for Future Feature Changes

Every new feature or behavior change should update this audit when it changes
one of these surfaces:

1. Public CLI flags or commands.
2. Adapter flags, environment variables, or Git config snippets.
3. Bridge request/response fields or auth states.
4. Credential provider behavior or secret storage keys.
5. Error codes, retryability, or tray status states.
6. Test maturity classification.
