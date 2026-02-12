# Git LFS Specification Gap Analysis

Date: 2026-02-09

## Baseline Sources

- `submodules/git-lfs/docs/custom-transfers.md`
- `submodules/git-lfs/docs/spec.md`
- `submodules/git-lfs/docs/api/batch.md`
- `submodules/git-lfs/docs/api/basic-transfers.md`
- `submodules/git-lfs/docs/api/locking.md`
- `submodules/git-lfs/tq/custom.go`
- `submodules/git-lfs/tq/custom_test.go`

## Scope Boundary

This repository currently implements a `custom transfer adapter` prototype with a `proton-drive-cli` subprocess bridge.
It does **not** implement a full LFS HTTP server (Batch API, basic transfer API, locking API).

Because of that boundary, full Git LFS API-spec coverage is currently impossible in this codebase without adding new components.

## Coverage Matrix

| Spec Area | Requirement | Status | Evidence | Gap |
| --- | --- | --- | --- | --- |
| Custom transfer protocol | Init request/ack shape | Covered | `cmd/adapter/main_test.go`, `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Upload/download message handling | Covered | `cmd/adapter/main_test.go`, `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Transfer-specific errors should not terminate process | Covered | `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Terminate has no response | Covered | `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Progress + complete event ordering | Covered | `cmd/adapter/main_test.go`, `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Progress byte semantics (`bytesSoFar`, `bytesSinceLast`) | Covered | `cmd/adapter/protocol_spec_test.go`, `tests/integration/git_lfs_custom_transfer_concurrency_stress_test.go` | None |
| Custom transfer protocol | Standalone mode (`action: null`) | Covered | `cmd/adapter/protocol_spec_test.go`, `tests/integration/git_lfs_custom_transfer_test.go` | None for local-store backend |
| Custom transfer protocol | Real `git-lfs` invocation path (black-box) | Covered (upload + download, local + sdk backend) | `tests/integration/git_lfs_custom_transfer_test.go`, `tests/integration/git_lfs_sdk_backend_test.go` | SDK backend uses proton-drive-cli subprocess directly; defaults still local |
| Adapter runtime backend contract | SDK init/upload/download wiring and error mapping | Covered (unit, mocked transport) | `cmd/adapter/backend_test.go` | None at adapter boundary |
| SDK bridge API contract | upload, download, list, refresh behavior | Covered | `tests/integration/git_lfs_sdk_backend_test.go` | proton-drive-cli uses per-operation auth and needs performance/lifecycle hardening |
| proton-drive-cli subprocess | Spawn lifecycle, JSON protocol, timeout, crash recovery | Covered | `cmd/adapter/bridge.go`, `cmd/adapter/bridge_test.go` | None |
| proton-drive-cli credential security | stdin credential passing, OID validation, path traversal | Covered | `tests/integration/credential_security_test.go` | None |
| proton-drive-cli integration | Auth, upload/download roundtrip, list, token refresh | Covered | `tests/integration/git_lfs_sdk_backend_test.go` | Requires valid Proton credentials via pass-cli |
| Custom transfer protocol | Wrong-OID progress/complete rejection (`git-lfs` side) | Covered | `tests/integration/git_lfs_custom_transfer_failure_modes_test.go` | None |
| Custom transfer protocol | Fatal subprocess behavior (crash/stall/partial write) | Covered | `tests/integration/git_lfs_custom_transfer_failure_modes_test.go`, `tests/integration/git_lfs_custom_transfer_timeout_semantics_test.go`, `cmd/adapter/protocol_spec_test.go` | None |
| Custom transfer protocol | Concurrent adapter process handling (`concurrent=true`) | Covered | `tests/integration/git_lfs_custom_transfer_concurrency_test.go`, `tests/integration/git_lfs_custom_transfer_concurrency_stress_test.go` | None |
| Custom transfer protocol | Direction config matrix (`upload`, `download`, `both`) at CLI level | Covered | `tests/integration/git_lfs_custom_transfer_config_matrix_test.go` | None |
| Pointer file spec (`submodules/git-lfs/docs/spec.md`) | Pointer generation/parsing, clean/smudge lifecycle | Not implemented in this repo | N/A | Depends on Git LFS core behavior and integration tests |
| Batch API (`submodules/git-lfs/docs/api/batch.md`) | Request/response schema, operation semantics, error codes | Not implemented in this repo | N/A | Requires LFS API server component |
| Basic transfer API (`submodules/git-lfs/docs/api/basic-transfers.md`) | Upload/download/verify HTTP behavior | Not implemented in this repo | N/A | Requires LFS API server component |
| Locking API (`submodules/git-lfs/docs/api/locking.md`) | Lock/unlock/list/verify semantics | Not implemented in this repo | N/A | Requires LFS API server component |

## Additional Tests Missing (High Value)

| Priority | Missing Test | Why It Matters |
| --- | --- | --- |
| P1 | Soak/load/failure-injection tests for proton-drive-cli subprocess | Validates stability, rate-limit behavior, and recovery semantics under sustained load |
| P2 | Streaming large file support (>2GB) | proton-drive-cli may timeout on very large files |

## Requirement Propagation To Proton SDK Layer

Yes, requirements can be derived from Git LFS specs and propagated down into the Proton SDK integration.

| Requirement ID | Git LFS Requirement | Adapter Requirement | Proton SDK/Service Requirement |
| --- | --- | --- | --- |
| CT-001 | Upload request includes `oid`, `size`, `path` | Validate and enforce request invariants | Upload API must accept deterministic input and preserve exact bytes |
| CT-002 | Download complete must return file `path` | Create and hand off durable temp file | SDK download must materialize bytes to local path safely |
| CT-003 | Transfer errors are per-object, not fatal to process | Return `complete` with `error` and continue | Service must return typed recoverable errors, not crash |
| CT-004 | Progress events track transfer progress | Emit valid progress events | SDK API must expose progress callbacks/byte counts |
| CT-005 | Standalone mode allows `action: null` | Resolve storage purely from object identity | Service must support deterministic OID-to-object mapping without batch action metadata |
| CT-006 | Verify action is outside custom transfer agent | Do not fake verify semantics in adapter | Service must expose enough metadata for upper layer verify handling |
| CT-007 | Protocol uses line-delimited JSON | Strict message framing and parsing | proton-drive-cli subprocess must avoid mixed stdout protocol contamination |

## Practical Answer To “Whole API Spec Coverage”

To truly cover the whole Git LFS feature set, we need to add:

1. An LFS API server implementation (or compliant proxy layer) for Batch/Basic/Locking APIs.
2. End-to-end tests running official Git LFS client workflows against that server.
3. A traceability matrix linking each spec clause to test IDs and implementation owners.

Until that exists, this repo can fully cover only the `custom transfer adapter` subset of the specification.
