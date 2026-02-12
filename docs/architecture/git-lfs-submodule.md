# Git LFS Submodule Integration Points

Submodule path: `submodules/git-lfs/`.

## Files To Track

- `submodules/git-lfs/tq/custom.go`: subprocess custom adapter runtime.
- `submodules/git-lfs/tq/manifest.go`: adapter registration and standalone mode.
- `submodules/git-lfs/docs/custom-transfers.md`: protocol contract.
- `submodules/git-lfs/docs/spec.md`: pointer/spec baseline.
- `submodules/git-lfs/tq/custom_test.go`: behavior reference tests.

## Why These Matter

- They define request/response semantics that adapter tests must enforce.
- They determine standalone behavior (`action: null`) and concurrency model.
- They are the authoritative compatibility target for this project.

## Practical Rule

When uncertain, treat upstream Git LFS behavior as source of truth and adapt local implementation/tests to match it.
