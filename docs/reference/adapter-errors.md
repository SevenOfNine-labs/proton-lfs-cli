# Adapter Errors Reference

Every failure the adapter reports has three faces: the message git-lfs
receives, the entry written to the [status file](status-file.md), and the
process exit status. This page lists all of them. Source of truth:
`cmd/adapter/main.go` (validation, `classifyError`, `statusForErrorCode`,
`classifyErrorByStatus`), `cmd/adapter/backend.go` (`ErrorCode`,
`BackendError`, `mapBridgeError`, `mapBridgeErrorCode`,
`mapAuthStateForTransfer`) and `cmd/adapter/bridge.go` (transport errors).

## Process exit status

| Status | When |
| --- | --- |
| 0 | git-lfs closed stdin (normal end of a transfer session) |
| 1 | A message on stdin could not be decoded (a protocol error is written first), or an internal invariant failed (progress accounting, stdout not writable) |
| 2 | Invalid flags, `PROTON_LFS_BRIDGE_TIMEOUT` not a positive Go duration, or an unsupported `--backend` value; nothing is read from stdin |

## Protocol-level errors

A protocol error is a message with only an `error` object
(`{"error":{"code":N,"message":"…"}}`), as the Git LFS custom transfer
specification prescribes for errors that are not tied to one object.

| Code | Message | Effect |
| --- | --- | --- |
| 1 | `failed to decode message: …` | Process exits with status 1 |
| 400 | `unknown event: <event>` | Message ignored, process continues |
| 400 | `invalid operation for init` | `init` refused; only `upload` and `download` are valid |
| 500 | `transfer backend is not configured` | `init` refused |
| backend code | message of the backend error | `init` refused: local backend 501 `local store backend is not configured` or 500 `failed to prepare local object store`; sdk backend one of the auth-state or bridge errors below (`failed to inspect proton drive auth state`, `failed to initialize lfs storage`) |

## Per-object errors

A transfer failure is reported as `{"event":"complete","oid":"…","error":{"code":N,"message":"…"}}`;
the process keeps serving the next object. The status file receives the same
message plus the classification described in the last section.

### Raised by the adapter before or after the backend call

These carry no backend code; the status-file `errorCode` comes from the
HTTP-style code.

| Code | Message | `errorCode` |
| --- | --- | --- |
| 400 | `session not initialized`, `invalid transfer size`, `invalid oid format`, `missing upload path`, `path traversal not allowed`, `null bytes not allowed in path` | `client_error` |
| 404 | `upload source file not found` | `not_found` |
| 409 | `upload size does not match transfer request`, `upload content hash does not match oid`, `downloaded object size does not match transfer request` | `conflict` |
| 500 | `failed to read upload source file`, `failed to validate downloaded object`, `downloaded object hash mismatch`, `transfer backend is not configured` | `server_error` |

### Raised by the local backend

The structured code is derived from the HTTP-style code (`classifyErrorCode`).

| Code | `errorCode` | Messages |
| --- | --- | --- |
| 404 | `not_found` | `object not found in local store` |
| 409 | `invalid_request` | `stored object size does not match transfer request` |
| 500 | `server_error` | `session not initialized`, `failed to prepare local object directory`, `failed to persist object in local store`, `failed to verify stored object`, `stored object hash mismatch`, `failed to read object from local store`, `failed to create temporary download file`, `failed to stage object for download`, `failed to finalize staged download object` |
| 501 | `server_error` | `local store backend is not configured` |

### Raised by the sdk backend from a bridge auth state

`init` runs `bridge auth-state`; the payload's `errors[]` are appended to the
message as `: <error>; <error>`.

| Auth state | Code | `errorCode` | Message |
| --- | --- | --- | --- |
| `ready` | none | none | transfers continue |
| `needs_login` | 401 | `auth_required` | `no ready Proton Drive session; run proton-drive-cli login before Git LFS transfer` |
| `session_expired` | 401 | `auth_required` | `Proton Drive session is expired; refresh or login before Git LFS transfer` |
| `session_invalid` | 401 | `auth_required` | `Proton Drive session is invalid; run proton-drive-cli login before Git LFS transfer` |
| `needs_key_password` | 401 | `key_password_required` | `stored browser-fork key password required for this Proton session` |
| `needs_data_password` | 401 | `data_password_required` | `mailbox/data password required for this Proton account` |
| `configuration_error` | 400 | `invalid_request` | `Proton Drive auth configuration error` |
| any other value | 502 | `server_error` | `unrecognized Proton Drive auth state` |
| payload missing | 502 | `server_error` | `proton drive auth state is unavailable` |
| `willAttemptNetwork: true` | 502 | `server_error` | `failed to inspect proton drive auth state: bridge auth-state must be offline-only` |

A transfer that arrives before a successful `init` fails with 401
`drive-cli backend is not authenticated` (`auth_required`).

### Raised by the sdk backend from a bridge error envelope

`details.errorCode` of the envelope is authoritative. The HTTP-style code is
the envelope's `code` (the bridge always sets it, from
`schemas/bridge/v1/error-code-map.json`); the column shows the value the
bridge emits.

| Bridge `errorCode` | Code | `errorCode` | Message sent to git-lfs |
| --- | --- | --- | --- |
| `AUTH_FAILED`, `INVALID_CREDENTIALS`, `SESSION_EXPIRED` | 401 | `auth_required` | `session is invalid or expired` |
| `TWO_FACTOR_REQUIRED` | 401 | `two_factor_required` | `two-factor authentication required` (`FIDO2 two-factor authentication required` when `details.twoFactorType` is `fido2`) |
| `KEY_PASSWORD_REQUIRED` | 401 | `key_password_required` | `stored browser-fork key password required for this Proton session` |
| `DATA_PASSWORD_REQUIRED` | 401 | `data_password_required` | `mailbox/data password required for this Proton account` |
| `CAPTCHA_REQUIRED` | 407 | `captcha_required` | `captcha verification required — run: proton-drive-cli login` |
| `RATE_LIMITED` | 429 | `rate_limited` | `rate limited by proton api — wait and retry` |
| `INSUFFICIENT_SCOPE` | 403 | `insufficient_scope` | `Proton rejected this app/session authorization scope` |
| `PERMISSION_DENIED` | 403 | `permission_denied` | `permission denied by drive backend` |
| `NOT_FOUND`, `FILE_NOT_FOUND`, `PATH_NOT_FOUND` | 404 | `not_found` | `object not found in drive backend` |
| `NETWORK_ERROR`, `CONNECTION_REFUSED` | 502 | `network_failure` | `drive service is unavailable` |
| `TIMEOUT` | 504 | `network_failure` | `drive service is unavailable` |
| `FILE_TOO_LARGE` | 413 | `invalid_request` | the bridge message |
| `INVALID_FILE`, `INVALID_PATH`, `NOT_A_FOLDER`, `VALIDATION_ERROR` | 400 | `invalid_request` | the bridge message |
| `OPERATION_CANCELLED` | 499 | `invalid_request` | the bridge message |
| `QUOTA_EXCEEDED`, `DISK_FULL` | 507 | `server_error` | the bridge message |
| `ENCRYPTION_FAILED`, `DECRYPTION_FAILED` | 500 | `server_error` | the bridge message; `retryable` and `temporary` are forced to false |
| `API_ERROR`, `UPLOAD_FAILED`, `DOWNLOAD_FAILED` | 502 | `server_error` | the fallback message of the operation (`drive-cli upload failed`, `drive-cli download failed`, `drive-cli existence check failed`, `failed to initialize lfs storage`) |
| `UNKNOWN_ERROR` | 500 | `server_error` | the fallback message of the operation |

For `exists`, a 404 envelope means "absent" and is not an error; any other
failure of the pre-upload existence check fails the upload.

An envelope whose `details` carry no `errorCode` is classified from its
message (two-factor, key password, data password, key unlock, scope) and
otherwise from its `code`: 401 `auth_required`, 403 `permission_denied`, 404
`not_found`, 407 `captcha_required`, 429 `rate_limited`, 503 and other 5xx
`server_error`, other 4xx `invalid_request` with the bridge message.

### Raised by the sdk backend without an envelope

When the subprocess cannot be started, is killed by the timeout, or prints no
valid envelope, the bridge client produces one of these messages and the
backend classifies it by keywords.

| Client message | Keyword rule | Code | `errorCode` | Message sent to git-lfs |
| --- | --- | --- | --- | --- |
| `bridge <command> timed out after <duration>` | typed timeout error | 504 | `network_failure` (retryable) | `bridge <command> timed out after <duration>; raise --bridge-timeout or PROTON_LFS_BRIDGE_TIMEOUT for large objects or slow links` |
| `bridge <command> failed: <stderr, redacted, 256 chars>` | first matching rule below | varies | varies | varies |
| `bridge <command>: empty stdout from bridge subprocess` | none | 502 | `server_error` | fallback message of the operation |
| `bridge <command>: no valid JSON envelope found in bridge output` | none | 502 | `server_error` | fallback message of the operation |
| `bridge <command>: invalid bridge response envelope: …` | none | 502 | `server_error` | fallback message of the operation |

Keyword rules, in order: two-factor/2fa/fido2 (401 `two_factor_required`);
key password (401 `key_password_required`); mailbox/data password, key
decryption (401 `data_password_required`); failed to decrypt, decrypt any
keys (401 `key_unlock_failed`); invalid or expired session, unauthorized
(401 `auth_required`); sufficient scope (403 `insufficient_scope`); not
found (404 `not_found`); captcha (407 `captcha_required`); rate limit (429
`rate_limited`); timeout, timed out, connection refused, no such host, dial
tcp (503 `server_error`, `drive service is unavailable`); otherwise 502 with
the operation's fallback message. Only words are matched: digits in free
text can belong to an OID and are never read as a status or Proton code.

For envelopes whose `details.errorCode` is the generic `UNKNOWN_ERROR` or
`DECRYPTION_FAILED`, the same keyword rules (the authentication ones and
Proton code 9101) are tried before the code table, so a wrong mailbox/data
password ("Failed to decrypt any keys") is reported as 401
`key_unlock_failed`, not as a retryable server error.

stderr fragments are truncated to 256 characters and cut at the first
`Bearer` (followed by a space), `token=`, `session=`, `AccessToken`,
`RefreshToken` or `UID:`.

## Status-file classification

The `state`, `errorCode` and `errorDetail` written for a failure follow this
order:

1. A structured backend code other than `unknown` decides: `captcha_required`
   gives `captcha`; `rate_limited` gives `rate_limited`;
   `two_factor_required`, `data_password_required`, `key_password_required`,
   `key_unlock_failed` and `auth_required` give `auth_required`; every other
   code gives `error`.
2. Without a structured code, the HTTP-style code and message decide: 407 or
   "CAPTCHA" gives `captcha`; 429 or "rate limit" gives `rate_limited`;
   two-factor, data-password and 401/403 keywords give `auth_required`; 404
   gives `not_found`, 409 `conflict`, 5xx `server_error`, other 4xx
   `client_error`, anything else `unknown_error`.
3. `retryable` is true only for `network_failure` and `server_error` with
   code 500, 502, 503 or 504; `temporary` is true when `retryable` is true or
   the code is `rate_limited`.

The `errorDetail` text for each code is listed in
[Status File Reference](status-file.md#errorcode-vocabulary).
