# Architecture Overview

## System Boundary

This repository implements a Git LFS custom transfer adapter that communicates with Proton Drive via proton-drive-cli subprocess.
It does not implement a full Git LFS HTTP server (`/objects/batch`, basic transfer API, locking API).

## Components

- `git-lfs` client: invokes the custom transfer agent.
- `cmd/adapter`: speaks line-delimited JSON protocol with Git LFS.
- `proton-drive-cli`: subprocess spawned by the adapter in `sdk` mode (JSON stdin/stdout protocol).
- Backend mode:
  - `local`: deterministic local object store for tests.
  - `sdk`: spawns `proton-drive-cli bridge <command>` for Proton Drive operations.

## Data Flow

Upload:

1. Git LFS sends `upload` event to adapter.
2. Adapter validates `oid`/`size`/`path`.
3. Adapter calls selected backend.
4. Adapter emits `complete` or `complete.error`.

Download:

1. Git LFS sends `download` event to adapter.
2. Adapter resolves object by `oid`.
3. Backend materializes file to local path.
4. Adapter returns `complete` with `path`.

## Non-Negotiables

- No successful transfer response without durable backend success.
- No production path may depend on mock success.
- `oid` and size integrity checks are mandatory.
