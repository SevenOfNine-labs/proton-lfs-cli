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
| `lastOp` | string | when known | `init`, `upload`, `download`, `terminate` (adapter) or `login` (tray) |
| `error` | string | on failure | Human-readable message, also sent to git-lfs |
| `errorCode` | string | on failure | Machine-readable code from the vocabulary below |
| `errorDetail` | string | on failure | Recovery guidance shown in the tray tooltip |
| `retryable` | bool | when true | A retry may succeed without user action |
| `temporary` | bool | when true | The failure is expected to be transient |
| `retryCount` | int | when non-zero | Declared in the structure; no writer sets it, so it is always omitted |
| `timestamp` | RFC 3339 | always | Set to the write time when the writer leaves it zero |

Example after a successful upload:

```json
{"state":"ok","lastOid":"3f8e…","lastOp":"upload","timestamp":"2026-11-06T10:21:33.412Z"}
```

Example after a refused transfer:

```json
{"state":"auth_required","lastOid":"3f8e…","error":"no ready Proton Drive session; run proton-drive-cli login before Git LFS transfer","errorCode":"auth_required","errorDetail":"Run: proton-drive-cli login to re-authenticate","timestamp":"2026-11-06T10:21:33.412Z"}
```

## States

| `state` | Written by | When | Tray icon |
| --- | --- | --- | --- |
| `idle` | adapter | `init` succeeded (`lastOp: init`) or `terminate` was received (`lastOp: terminate`) | green while signed in and ready, otherwise the readiness icon |
| `ok` | adapter | An upload or download completed (`lastOid`, `lastOp` set) | green |
| `transferring` | nobody at present | Reserved for a transfer in progress; the tray renders it (blue icon, "Uploading…"/"Downloading…" tooltip) but the current adapter writes `ok` or a failure state only when a transfer ends | blue |
| `error` | adapter | A transfer failed for a reason that is not one of the three states below | red |
| `rate_limited` | adapter, tray | Proton rate-limited the request (`errorCode: rate_limited`), or the tray's Connect was rate-limited (`lastOp: login`, `errorCode: RATE_LIMITED`); the tray refuses another login for one hour after the timestamp | red |
| `auth_required` | adapter | The saved session or local unlock material is not usable (`auth_required`, `two_factor_required`, `data_password_required`, `key_password_required`, `key_unlock_failed`) | red |
| `captcha` | adapter | Proton requires CAPTCHA verification (`captcha_required`) | red |

The tray has four icons (idle, ok, error, syncing). `rate_limited`,
`auth_required` and `captcha` share the error icon and are told apart by the
tooltip. While no session is signed in, or local readiness is blocked, the
tray shows its readiness state instead of the transfer state.

## errorCode vocabulary

A structured backend code is authoritative; the HTTP-style status and the
message are used only when the failure carries none.

| `errorCode` | Source | `state` | `errorDetail` |
| --- | --- | --- | --- |
| `captcha_required` | bridge `CAPTCHA_REQUIRED`, status 407, or "CAPTCHA" in the message | `captcha` | Run: proton-drive-cli login to complete CAPTCHA verification |
| `rate_limited` | bridge `RATE_LIMITED`, status 429, or "rate limit" in the message | `rate_limited` | Wait before retrying operations. The Proton API has rate-limited requests. |
| `two_factor_required` | bridge `TWO_FACTOR_REQUIRED` or "two-factor", "2fa", "fido2" in the message | `auth_required` | Complete two-factor authentication in the browser during proton-drive-cli login |
| `data_password_required` | auth-state `needs_data_password`, bridge `DATA_PASSWORD_REQUIRED`, or "data password" in the message | `auth_required` | Configure a mailbox/data password provider (--data-credential-provider); do not retry account login |
| `key_password_required` | auth-state `needs_key_password`, bridge `KEY_PASSWORD_REQUIRED`, or "key password" in the message | `auth_required` | Run: proton-drive-cli login with the configured key-password provider |
| `key_unlock_failed` | "failed to decrypt" or "decrypt any keys" in the message | `auth_required` | The stored mailbox/data password did not unlock the Proton keys; store the correct password |
| `auth_required` | auth-state `needs_login`, `session_expired`, `session_invalid`; bridge `AUTH_FAILED`, `SESSION_EXPIRED`, `INVALID_CREDENTIALS`; status 401 or 403 | `auth_required` | Run: proton-drive-cli login to re-authenticate |
| `insufficient_scope` | bridge `INSUFFICIENT_SCOPE`, Proton code 9101 | `error` | Proton rejected the app/session scope; run proton-lfs-cli scope-diagnostics and do not retry login |
| `permission_denied` | bridge `PERMISSION_DENIED`, envelope status 403 without a code | `error` | Proton Drive denied access to this object |
| `not_found` | bridge `NOT_FOUND`, `FILE_NOT_FOUND`, `PATH_NOT_FOUND`; status 404 | `error` | The requested object was not found in Proton Drive |
| `network_failure` | bridge `NETWORK_ERROR`, `TIMEOUT`, `CONNECTION_REFUSED` | `error` | Proton Drive is unreachable; the operation may be retried |
| `server_error` | bridge `API_ERROR`, `UPLOAD_FAILED`, `DOWNLOAD_FAILED`, `UNKNOWN_ERROR`, `QUOTA_EXCEEDED`, `DISK_FULL`, `ENCRYPTION_FAILED`, `DECRYPTION_FAILED`; any 5xx | `error` | Proton Drive reported a server error; the operation may be retried |
| `invalid_request` | bridge `FILE_TOO_LARGE`, `INVALID_FILE`, `INVALID_PATH`, `NOT_A_FOLDER`, `VALIDATION_ERROR`, `OPERATION_CANCELLED`; auth-state `configuration_error`; other 4xx | `error` | Client error - check request parameters |
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

- Tooltip: `Proton LFS — Last <lastOp> <relative time>` for `ok`;
  `Proton LFS — <errorCode>: <errorDetail>` for `error`; dedicated texts for
  `rate_limited`, `auth_required`, `captcha` and `transferring`;
  `(retryable, temporary)` is appended when those flags are set.
- `proton-lfs-tray status` prints a `Transfer:` line built from the same
  fields, for example `upload 3m ago (ok)` or
  `download just now (object not found in drive backend; code=not_found)`.
- A missing or unreadable file is shown as `Transfer: no data`; the adapter
  recreates it on the next `init`.
