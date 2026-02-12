# Project Plan: Proton Git LFS Backend

## Objective

Deliver a production-capable Git LFS backend that stores LFS objects in Proton Drive without violating Git LFS protocol guarantees, integrity guarantees, or Proton SDK constraints.

## Definition Of Done

The backend is "done" only when all conditions below are met:

1. Every successful upload/download is real and verifiable end-to-end.
2. No mock path is reachable in production builds.
3. Data integrity is enforced (`oid`, size, and transport checks).
4. Auth/session handling is secure and auditable.
5. Operational SLOs are defined, measured, and met.
6. Disaster recovery and rollback procedures are tested.

## Current Baseline (2026-02-10)

| Area | Status | Notes |
| --- | --- | --- |
| Adapter protocol loop | Yellow | Basic flow exists; real transfer implementation absent |
| Proton data path | Yellow | Local prototype remains default; proton-drive-cli bridge available via direct subprocess invocation |
| Auth/session design | Yellow | pass-cli mandatory for credential resolution; no direct env var fallback |
| Test quality | Yellow | Go tests improved; mocked E2E pipeline tests added |
| CI/CD | Yellow | SDK unit tests and mocked E2E jobs added; coverage and release hardening still needed |
| Security posture | Yellow | Credential lockdown via pass-cli, threat model documented, security tests in place; audit still needed |

## Delivery Phases

## Phase 0: Foundation Hardening

- Outcome: fail-closed defaults, honest project status, reliable CI.
- Exit criteria:
  - Adapter cannot claim successful transfer unless explicitly in mock mode.
  - CI build/lint/test runs on all PRs.
  - Plan and risk register are tracked in-repo.

## Phase 1: Protocol Correctness

- Outcome: strict Git LFS custom transfer compliance.
- Exit criteria:
  - Full protocol fixtures for `init/upload/download/terminate/error`.
  - Concurrency behavior verified with deterministic tests.
  - Error codes and response shapes validated against spec.

## Phase 2: Real Storage Path

- Outcome: real Proton-backed upload/download implementation.
- Exit criteria:
  - Upload writes encrypted content to Proton and returns only after durable success.
  - Download returns verified content matching pointer `oid` and size.
  - OID-to-storage mapping strategy documented and migration-safe.

## Phase 3: Identity, Auth, and Secrets

- Outcome: secure authentication and token lifecycle.
- Exit criteria:
  - No plaintext credentials in source/config defaults.
  - Token refresh/revocation flow implemented with hard expiration handling.
  - Secret sources documented for local dev and CI runtime.

## Phase 4: Operations and Resilience

- Outcome: production operations baseline.
- Exit criteria:
  - Structured logs with correlation IDs.
  - Metrics for transfer latency, error rate, and throughput.
  - Retry/backoff policies and circuit-breaking behavior implemented.
  - Runbooks for incident response and rollback exist.

## Phase 5: Production Readiness Gate

- Outcome: controlled production launch.
- Exit criteria:
  - Threat model and security review closed.
  - Load and soak tests completed at target scale.
  - Backup/restore tests passed.
  - Release checklist and owner sign-off completed.

## Non-Negotiable Engineering Gates

- No "simulated success" in production paths.
- No hidden network dependencies in tests.
- No release without reproducible build artifacts.
- No PR merge with failing lint/test workflows.

## Operating Cadence

- Weekly: risk review, milestone burn-down, blocked-item escalation.
- Per PR: protocol compliance check, regression test check, security checklist.
- Per release: artifact verification, changelog, rollback plan.
