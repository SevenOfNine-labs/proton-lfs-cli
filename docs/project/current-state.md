# Current State

Date: 2026-02-10

## Implemented

- Adapter protocol loop (`init`, `upload`, `download`, `terminate`) is implemented and testable.
- Local backend is usable for deterministic end-to-end integration tests.
- SDK backend path is wired and covered with integration tests against `proton-drive-cli` (direct subprocess).
- The Go adapter spawns `proton-drive-cli bridge <command>` directly via stdin/stdout JSON, replacing the former Node.js HTTP bridge layer.
- SDK integration suite covers upload, download, list, and token refresh operations.
- Credential providers (`pass-cli`, `git-credential`) are handled by proton-drive-cli's `src/credentials/` module.
- Security hardening: OID validation, path traversal prevention, subprocess pool (max 10), per-operation timeout (5 min).
- Security tests: command injection, rate limiting, credential flow, session file permissions.

## Architecture

```
Go Adapter → proton-drive-cli subprocess (stdin/stdout JSON, credentialProvider field) → Proton API
                    ↓
        pass-cli or git-credential
    (resolved internally by proton-drive-cli)
```

- **No .NET SDK or Node.js HTTP bridge required.** The Go adapter spawns `proton-drive-cli bridge <command>` directly.
- The former `proton-lfs-bridge` Node.js HTTP layer has been removed.

## Not Implemented Yet

- Production-grade session lifecycle (session refresh has known issues in proton-drive-cli).
- Production observability baseline (metrics, SLOs, alerts, runbooks).
- Streaming support for very large files (>2GB may timeout).

## Local Baseline

```bash
make setup
make build-all        # Builds Go adapter, Git LFS, and proton-drive-cli
make test
make test-integration
```

SDK integration path:

```bash
eval "$(make -s pass-env)"
make test-integration-sdk
```

SDK backend with proton-drive-cli:

```bash
eval "$(make -s pass-env)"
make check-sdk-prereqs
make test-integration-sdk
```
