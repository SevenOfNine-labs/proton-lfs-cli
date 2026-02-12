# Documentation

Project-owned docs live here. Upstream/vendor docs remain under `submodules/` and are referenced, not duplicated.

## Start Here

1. `docs/project/goal.md`
2. `docs/project/current-state.md`
3. `docs/project/project-plan.md`
4. `docs/architecture/sdk-capability-matrix.md`
5. `docs/testing/spec-gap-analysis.md`

## Structure

- `docs/project/`: goals, plan, risks, and current status.
- `docs/architecture/`: component boundaries and protocol contracts.
- `docs/operations/`: runtime configuration, deployment, and release guidance.
- `docs/testing/`: test strategy, coverage gaps, and requirement traceability.
- `docs/reference/`: concise scope references to external specs.

Machine-readable requirement mapping: `docs/testing/spec-requirements.yaml`.

## Canonical Rule

When docs disagree, runtime behavior and tests win. Update docs in the same change.
