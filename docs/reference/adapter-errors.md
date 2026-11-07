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
message as `: <error>; <error>`. A state other than `ready` and
`session_expired` refuses `init` with a protocol error carrying the code and message below; the `errorCode`
column is the backend's classification, and the status file is not written
for a refused `init`. The states and their conditions are listed in
[Subprocess Integration](../architecture/subprocess-integration.md#auth-state-payload).

| Auth state | Code | `errorCode` | Message |
| --- | --- | --- | --- |
| `ready` | none | none | transfers continue |
| `session_expired` | none | none | transfers continue; `bridge init` refreshes the access token before use |
| `needs_login` | 401 | `auth_required` | `no ready Proton Drive session; run proton-drive-cli login before Git LFS transfer` |
| `session_invalid` | 401 | `auth_required` | `Proton Drive session is invalid; run proton-drive-cli login before Git LFS transfer` |
| `credentials_unavailable` | 401 | `credentials_unavailable` | `Proton session credentials are unavailable from the credential store` |
| any other value | 502 | `server_error` | `unrecognized Proton Drive auth state` |
| payload missing, without `state`, not parsable, or with `willAttemptNetwork: true` | 502 | `server_error` | `failed to inspect proton drive auth state` |

A failed `bridge auth-state` envelope or transport failure is classified like
any other bridge error (tables below) with the fallback message
`failed to inspect proton drive auth state`.

A transfer that arrives before a successful `init` fails with 400
`session not initialized` (the adapter's own check above).

### Raised by the sdk backend from a bridge error envelope

`details.errorCode` of the envelope is authoritative. The HTTP-style code is
the envelope's `code` (the bridge always sets it, from
`schemas/bridge/v1/error-code-map.json`); the column shows the value the
bridge emits.

| Bridge `errorCode` | Code | `errorCode` | Message sent to git-lfs |
| --- | --- | --- | --- |
| `AUTH_FAILED`, `SESSION_EXPIRED` | 401 | `auth_required` | `session is invalid or expired` |
| `CREDENTIALS_UNAVAILABLE` | 401 | `credentials_unavailable` | `Proton session credentials are unavailable from the credential store` |
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
| `API_ERROR`, `UPLOAD_FAILED`, `DOWNLOAD_FAILED` | 502 | `server_error` | the fallback message of the operation (`drive-cli upload failed`, `drive-cli download failed`, `drive-cli existence check failed`, `failed to initialize lfs storage`, `failed to inspect proton drive auth state`) |
| `UNKNOWN_ERROR` | 500 | `server_error` | the fallback message of the operation |

These are the 27 codes of proton-drive-cli's
`schemas/bridge/v1/error-code-map.json`. An `errorCode` outside that map is
classified by the envelope `code` as described below; the envelope message
never decides the classification.

For `exists`, a 404 envelope means "absent" and is not an error; any other
failure of the pre-upload existence check fails the upload.

`details.errorCode` is always present (`error-details.schema.json`); for a
code outside the map the envelope `code` decides:

| Envelope `code` | `errorCode` | Message sent to git-lfs |
| --- | --- | --- |
| 401 | `auth_required` | `session is invalid or expired` |
| 403 | `permission_denied` | `permission denied by drive backend` |
| 404 | `not_found` | `object not found in drive backend` |
| 407 | `captcha_required` | `captcha verification required — run: proton-drive-cli login` |
| 429 | `rate_limited` | `rate limited by proton api — wait and retry` |
| 503 | `server_error` | `drive service is unavailable` |
| other 5xx | `server_error` | the fallback message of the operation |
| other 4xx | `invalid_request` | the bridge message |
| below 400 | `server_error` (code 502) | the fallback message of the operation |

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

A bridge executable that cannot be started (any `Cmd.Start` failure of
`os/exec`: missing, not executable, not loadable) is a `BridgeStartError`,
mapped to 400 `invalid_request` with `proton-drive-cli could not be started;
check --drive-cli-bin, PROTON_DRIVE_CLI_BIN and NODE_BIN`, before any keyword
rule.

Keyword rules, in order, on the lower-cased message: credential store, key
password (401 `credentials_unavailable`); invalid or expired session, unauthorized (401
`auth_required`); sufficient scope (403 `insufficient_scope`); not found
(404 `not_found`); captcha (407 `captcha_required`); rate limit (429
`rate_limited`); timeout, timed out, connection refused, no such host, dial
tcp (503 `server_error`, `drive service is unavailable`); otherwise 502 with
the operation's fallback message. Only words are matched: digits in free
text can belong to an OID and are never read as a status or Proton code.

stderr fragments are redacted by `bridgeproto.RedactText` (bearer tokens,
secret-named fields in JSON, query and log forms, URL userinfo) and then cut
to 256 runes.

## Status-file classification

The `state`, `errorCode` and `errorDetail` written for a failure follow this
order:

1. A structured backend code other than `unknown` decides
   (`statusForErrorCode`): `captcha_required` gives `captcha`;
   `rate_limited` gives `rate_limited`; `credentials_unavailable` and
   `auth_required` give `auth_required`; every other
   code gives `error`.
2. Without a structured code (`classifyErrorByStatus`), the HTTP-style code
   and message decide: 407 or "CAPTCHA" gives `captcha`; 429 or "rate limit"
   gives `rate_limited`; 401, 403 or "Authentication" gives `auth_required`;
   404 gives `not_found`, 409 `conflict`, 5xx `server_error`, other 4xx
   `client_error`, anything else `unknown_error`.
3. `retryable` is true only for `network_failure` and `server_error` with
   code 500, 502, 503 or 504; `temporary` is true when `retryable` is true or
   the code is `rate_limited`.

The `errorDetail` text for each code is listed in
[Status File Reference](status-file.md#errorcode-vocabulary).
