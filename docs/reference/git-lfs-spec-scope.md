# Git LFS Spec Scope For This Repo

## In Scope

- Custom transfer adapter protocol compatibility.
- Standalone custom transfer agent behavior.
- Adapter/backend contract and error semantics.

## Out Of Scope (Current Codebase)

- Full Git LFS HTTP server implementation:
  - batch API
  - basic transfer API
  - locking API

## Upstream Sources

- `submodules/git-lfs/docs/custom-transfers.md`
- `submodules/git-lfs/docs/spec.md`
- `submodules/git-lfs/docs/api/batch.md`
- `submodules/git-lfs/docs/api/basic-transfers.md`
- `submodules/git-lfs/docs/api/locking.md`

## Practical Implication

"Whole Git LFS API coverage" is not achievable in this repository until an LFS API server component is added.
