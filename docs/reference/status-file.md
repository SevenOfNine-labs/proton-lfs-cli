# Status File Reference

The adapter reports the outcome of its work to `~/.proton-lfs/status.json`;
the tray reads it every 5 seconds. Source of truth:
`internal/config/status.go` (structure, states, write/read),
`cmd/adapter/main.go` (what the adapter writes, `classifyError`),
`cmd/adapter/backend.go` (`ErrorCode` values), `cmd/tray/status.go` and
`cmd/tray/connect.go` (what the tray reads and writes).

## Location and permissions

| Aspect | Rule |
| --- | --- |
| Default path | `$HOME/.proton-lfs/status.json` |
| Override | `PROTON_LFS_STATUS_FILE=<path>` (used verbatim) |
| Home directory | Resolved with `os.UserHomeDir`; when it fails (no `HOME` under cron or systemd) the write fails and is not retried against the working directory |
| Directory mode | `0700` (created on demand) |
| File mode | `0600` |
| Write | Atomic: temporary file in the same directory, `fsync`, rename over the target. Readers see the previous or the complete new content, never a partial file |
| Write errors | Ignored by the adapter (status is advisory, never a transfer failure) |

## Fields

| Field | Type | Present | Meaning |
| --- | --- | --- | --- |
| `state` | string | always | One of the states below |
| `lastOid` | string | when known | OID of the object the report is about |
| `lastOp` | string | when known | `upload` or `download` (adapter; on a failure the operation of the session) or `login` (tray) |
| `error` | string | on failure | Human-readable message, also sent to git-lfs |
| `errorCode` | string | on failure | Machine-readable code from the vocabulary below |
| `errorDetail` | string | on failure | Recovery guidance shown in the tray tooltip |
| `retryable` | bool | when true | A retry may succeed without user action |
| `temporary` | bool | when true | The failure is expected to be transient |
| `timestamp` | RFC 3339 | always | Set to the write time when the writer leaves it zero |

Example after a successful upload:

```json
{"state":"ok","lastOid":"3f8e…","lastOp":"upload","timestamp":"2026-11-06T10:21:33.412Z"}
```

Example after a transfer whose bridge command reported an expired session:

```json
{"state":"auth_required","lastOid":"3f8e…","lastOp":"upload","error":"session is invalid or expired","errorCode":"auth_required","errorDetail":"Run: proton-drive-cli login to re-authenticate","timestamp":"2026-11-06T10:21:33.412Z"}
```

Only transfer results are written: `init` and `terminate` leave the file as
it is, so the result of the last transfer survives the end of its session.
A refused `init` (the `bridge auth-state` gate, an unconfigured backend) is
answered with a protocol error to git-lfs; the tray derives sign-in
readiness from the session file itself
([Tray Reference](tray.md#readiness-rules)).

## States

| `state` | Written by | When | Tray icon |
| --- | --- | --- | --- |
| `ok` | adapter | An upload or download completed (`lastOid`, `lastOp` set) | the readiness icon (green while signed in and ready) |
| `transferring` | no component | Declared in `internal/config/status.go` for a transfer in progress and rendered by the tray (blue icon, "Uploading…"/"Downloading…" tooltip); the adapter writes `ok` or a failure state when a transfer ends and never writes `transferring` | blue |
| `error` | adapter | A transfer failed for a reason that is not one of the three states below | red |
| `rate_limited` | adapter, tray | Proton rate-limited the request (`errorCode: rate_limited`), or the tray's Connect was rate-limited (`lastOp: login`, `errorCode: RATE_LIMITED`); the tray refuses another login for one hour after the timestamp | red |
| `auth_required` | adapter | A transfer failed because the session or its credential-store entry is not usable (`auth_required`, `credentials_unavailable`) | red |
| `captcha` | adapter | Proton requires CAPTCHA verification (`captcha_required`) | red |

The tray has four icons (idle, ok, error, syncing). `rate_limited`,
`auth_required` and `captcha` share the error icon and are told apart by the
tooltip. While no session is signed in, local readiness is blocked, or the
file is missing or reports `ok`, the tray shows its readiness instead of the
transfer state. An `auth_required` or `captcha` report, or a `rate_limited`
report of a login, older than a login the tray saw while running (a new
session `uid`) is also replaced by the readiness.

## errorCode vocabulary

A structured backend code is authoritative; the HTTP-style status and the
message are used only when the failure carries none.

| `errorCode` | Source | `state` | `errorDetail` |
| --- | --- | --- | --- |
| `captcha_required` | bridge `CAPTCHA_REQUIRED`, status 407, or "CAPTCHA" in the message | `captcha` | Run: proton-drive-cli login to complete CAPTCHA verification |
| `rate_limited` | bridge `RATE_LIMITED`, status 429, or "rate limit" in the message | `rate_limited` | Wait before retrying operations. The Proton API has rate-limited requests. |
| `credentials_unavailable` | bridge `CREDENTIALS_UNAVAILABLE` (session entry unreadable from the credential store, or a key password not unlocking the account keys), or "credential store" or "key password" in a transport message | `auth_required` | Fix the credential store of the Proton session, or run: proton-drive-cli login |
| `auth_required` | bridge `AUTH_FAILED`, `SESSION_EXPIRED`; envelope status 401 with an unmapped code; "invalid or expired session" or "unauthorized" in a transport message; without a backend code, status 401 or 403 or "Authentication" in the message | `auth_required` | Run: proton-drive-cli login to re-authenticate |
| `insufficient_scope` | bridge `INSUFFICIENT_SCOPE`, or "sufficient scope" in a transport message | `error` | Proton rejected the app/session scope; run proton-lfs-cli scope-diagnostics and do not retry login |
| `permission_denied` | bridge `PERMISSION_DENIED`, envelope status 403 with an unmapped code | `error` | Proton Drive denied access to this object |
| `not_found` | bridge `NOT_FOUND`, `FILE_NOT_FOUND`, `PATH_NOT_FOUND`; without a backend code, status 404 | `error` | The requested object was not found in Proton Drive; without a backend code: The requested resource was not found |
| `network_failure` | bridge `NETWORK_ERROR`, `TIMEOUT`, `CONNECTION_REFUSED`; a bridge command killed at `--bridge-timeout` | `error` | Proton Drive is unreachable; the operation may be retried |
| `server_error` | bridge `API_ERROR`, `UPLOAD_FAILED`, `DOWNLOAD_FAILED`, `UNKNOWN_ERROR`, `QUOTA_EXCEEDED`, `DISK_FULL`, `ENCRYPTION_FAILED`, `DECRYPTION_FAILED`; without a backend code, any 5xx | `error` | Proton Drive reported a server error; the operation may be retried; without a backend code: Internal server error - the operation may be retried (500) or Server error - the operation may be retried (other 5xx) |
| `invalid_request` | bridge `FILE_TOO_LARGE`, `INVALID_FILE`, `INVALID_PATH`, `NOT_A_FOLDER`, `VALIDATION_ERROR`, `OPERATION_CANCELLED`; other 4xx from a backend (local store size mismatch, envelope with an unmapped code) | `error` | Client error - check request parameters |
| `conflict` | adapter-side size or hash mismatch (status 409, no backend code) | `error` | Resource conflict (e.g., hash or size mismatch) |
| `client_error` | other 4xx without a backend code | `error` | Client error - check request parameters |
| `unknown_error` | no code and no recognisable status | `error` | empty |
| `RATE_LIMITED` | tray, after a rate-limited Connect | `rate_limited` | Proton blocked the sign-in attempt. Wait before retrying; avoid VPN/shared exit IPs for the next canary. |

## retryable and temporary

- `retryable` is true only for `network_failure` and `server_error` with
  status 500, 502, 503 or 504 (RFC 9110 §15.6). Quota exhaustion (507) and
  local cryptographic failures (`ENCRYPTION_FAILED`, `DECRYPTION_FAILED`,
  status 500) are never retryable.
- `temporary` is true when `retryable` is true or the code is
  `rate_limited`.
- The adapter does not retry on its own; the flags describe the failure for
  the tray and for git-lfs users. The bridge itself retries a failed upload or
  download up to three times with 1-5 s backoff before reporting.

## How the tray renders it

- Tooltip: the readiness tooltip for `ok`;
  `Proton LFS — <errorCode>: <errorDetail>` for `error`; dedicated texts for
  `rate_limited`, `auth_required`, `captcha` and `transferring`;
  `(retryable, temporary)` is appended when those flags are set.
- `proton-lfs-tray status` prints a `Transfer:` line built from the same
  fields, for example `upload 3m ago (ok)` or
  `download just now (object not found in drive backend; code=not_found)`.
- A missing or unreadable file is shown as `Transfer: no data`; the adapter
  recreates it with the next transfer result.
