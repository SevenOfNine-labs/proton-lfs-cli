# Gap Analysis: Git LFS Spec Compliance & Feature Mapping

## 1. Git LFS Custom Transfer Spec Compliance

Reference: `submodules/git-lfs/docs/custom-transfers.md`

| Spec Requirement | Status | Location |
| --- | --- | --- |
| Line-delimited JSON on stdin/stdout | DONE | `main.go` json.Encoder/Decoder |
| `init` with operation/remote/concurrent | DONE | `handleInit()` |
| `init` success returns `{}` | DONE | `handleInit()` |
| `init` error returns `{error: {code, message}}` | DONE | `sendProtocolError()` |
| `upload` with oid/size/path/action | DONE | `handleUpload()` |
| Upload progress (monotonic bytesSoFar) | DONE (synthetic) | `sendProgressSequence()` |
| Upload complete with oid | DONE | `handleUpload()` |
| Upload SHA-256 verification | DONE | `calculateFileSHA256()` |
| `download` with oid/size/action | DONE | `handleDownload()` |
| Download complete with oid + path | DONE | Returns temp file path |
| Download progress | DONE (synthetic) | `sendProgressSequence()` |
| Download SHA-256 verification | DONE (bonus) | Spec says not required for custom agents |
| `terminate` clean shutdown | DONE | `handleTerminate()` |
| No response after terminate | DONE | Returns nil, no encode |
| Per-object errors (no process exit) | DONE | `sendTransferError()` |
| Fatal errors to stderr + non-zero exit | DONE | `log.Fatalf()` |
| Standalone mode (action field null) | DONE | action field ignored |
| Concurrent instances | DONE | git-lfs spawns N adapter processes |
| Verify action | N/A | Spec: custom agents don't handle verify |

## 2. Adapter Features to Bridge Commands

| Adapter Feature | Bridge Command | Status |
| --- | --- | --- |
| Auth (pass-cli) | `bridge auth` | DONE |
| Auth (git-credential) | `bridge auth` | DONE |
| LFS storage init | `bridge init` | DONE |
| Upload | `bridge upload` | DONE |
| Download | `bridge download` | DONE |
| Exists (dedup) | `bridge exists` | DONE (per-upload check) |
| Batch exists | `bridge batch-exists` | AVAILABLE but unused by adapter |
| List | `bridge list` | AVAILABLE but unused by adapter |
| Delete | `bridge delete` | AVAILABLE but unused by adapter |
| Batch delete | `bridge batch-delete` | AVAILABLE but unused by adapter |
| Session refresh | `bridge refresh` | AVAILABLE but unreliable |

## 3. Production Readiness Gaps

### Must-have

1. **Real-time progress** — Progress events are currently synthetic, emitted after the transfer completes. Needs proton-drive-cli to stream progress during upload/download.

2. **Session reuse** — Each bridge command spawns a fresh Node.js process and performs a full SRP auth flow. Transferring 10 files means 10 separate auth handshakes.

3. **Retry on transient failure** — No retry logic for 503, timeout, or network errors. A single transient failure fails the entire object transfer.

### Should-have

1. **Batch pre-flight** — `bridge batch-exists` is available but the adapter checks existence one object at a time. Batching dedup checks before upload would reduce round-trips.

2. **Garbage collection** — `bridge batch-delete` is available but no prune workflow is exposed to the user (e.g., `git lfs prune` equivalent for Proton Drive).

3. **Session refresh reliability** — Known issue noted in proton-drive-cli README: session refresh does not work reliably.

### Won't-have (per spec)

1. **Verify action** — The spec states custom transfer agents do not handle verify; git-lfs handles this via its own batch API.

2. **Lock/unlock** — The spec states custom transfer agents do not handle locking; this is a batch API concern.
