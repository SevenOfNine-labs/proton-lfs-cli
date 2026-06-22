# Documentation

Project-owned docs live here. Upstream/vendor docs remain under `submodules/` and are referenced, not duplicated.

## Start Here

1. `docs/project/current-state.md`
2. `docs/architecture/feature-audit.md`
3. `docs/architecture/sdk-capability-matrix.md`
4. `docs/architecture/proton-auth-hardening-plan.md`
5. `docs/architecture/proton-auth-news-audit-2026-06-21.md`
6. `docs/operations/live-canary-runbook.md`
7. `docs/operations/tray-platform-release-checklist.md`

## Structure

- `docs/project/`: goals, plan, risks, and current status.
- `docs/architecture/`: component boundaries and protocol contracts.
- `docs/operations/`: runtime configuration, deployment, and release guidance.
- `docs/testing/`: test strategy, coverage gaps, and requirement traceability.

Machine-readable requirement mapping: `docs/testing/spec-requirements.yaml`.

## Canonical Rule

When docs disagree, runtime behavior and tests win. Update docs in the same change.
