// Package main implements the git-lfs-proton-adapter, a Git LFS custom
// transfer agent for Proton Drive.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TransferBackend defines the storage/runtime backend used by adapter transfers.
type TransferBackend interface {
	Initialize(session *Session) error
	Upload(session *Session, oid, sourcePath string, expectedSize int64) (int64, error)
	// Download stages the object in a temporary file; expectedSize is the
	// size announced by git-lfs (0 when unknown) and sizes the bridge timeout.
	Download(session *Session, oid string, expectedSize int64) (string, int64, error)
}

// ProgressFunc reports transfer progress using Git LFS custom-transfer byte
// semantics: bytesSoFar is cumulative and bytesSinceLast is the increment.
type ProgressFunc func(bytesSoFar, bytesSinceLast int64) error

// ProgressTransferBackend is implemented by backends that can emit progress
// while their own transfer/staging work is in progress.
type ProgressTransferBackend interface {
	TransferBackend
	UploadWithProgress(session *Session, oid, sourcePath string, expectedSize int64, progress ProgressFunc) (int64, error)
	DownloadWithProgress(session *Session, oid string, expectedSize int64, progress ProgressFunc) (string, int64, error)
}

// ErrorCode is a machine-readable error classification for structured error handling.
type ErrorCode string

const (
	ErrCodeNetworkFailure      ErrorCode = "network_failure"
	ErrCodeAuthRequired        ErrorCode = "auth_required"
	ErrCodeRateLimited         ErrorCode = "rate_limited"
	ErrCodeCaptchaRequired     ErrorCode = "captcha_required"
	ErrCodeKeyPasswordRequired ErrorCode = "key_password_required"
	ErrCodeInsufficientScope   ErrorCode = "insufficient_scope"
	ErrCodeNotFound            ErrorCode = "not_found"
	ErrCodePermissionDenied    ErrorCode = "permission_denied"
	ErrCodeServerError         ErrorCode = "server_error"
	ErrCodeInvalidRequest      ErrorCode = "invalid_request"
	ErrCodeUnknown             ErrorCode = "unknown"
)

// BackendError maps backend-specific failures to protocol-safe transfer errors.
type BackendError struct {
	Code      int       // HTTP-style status code
	Message   string    // User-friendly message
	Err       error     // Underlying error
	ErrorCode ErrorCode // Machine-readable error code
	Retryable bool      // Whether operation can be retried
	Temporary bool      // Whether error is transient
}

func (e *BackendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *BackendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newBackendError(code int, message string, err error) error {
	return newBackendErrorWithCode(code, message, err, classifyErrorCode(code))
}

func newBackendErrorWithCode(code int, message string, err error, errorCode ErrorCode) error {
	retryable := isRetryable(errorCode, code)
	return &BackendError{
		Code:      code,
		Message:   message,
		Err:       err,
		ErrorCode: errorCode,
		Retryable: retryable,
		Temporary: retryable || errorCode == ErrCodeRateLimited,
	}
}

// classifyErrorCode maps HTTP status codes to structured error codes
func classifyErrorCode(httpCode int) ErrorCode {
	switch httpCode {
	case 401:
		return ErrCodeAuthRequired
	case 404:
		return ErrCodeNotFound
	case 407:
		return ErrCodeCaptchaRequired
	case 429:
		return ErrCodeRateLimited
	case 503:
		return ErrCodeServerError
	default:
		if httpCode >= 500 {
			return ErrCodeServerError
		}
		if httpCode >= 400 {
			return ErrCodeInvalidRequest
		}
		return ErrCodeUnknown
	}
}

// isRetryable admits only transport failures and upstream server errors
// (RFC 9110 §15.6: 500, 502, 503, 504); 507 and local crypto failures need
// the user.
func isRetryable(errorCode ErrorCode, code int) bool {
	switch errorCode {
	case ErrCodeNetworkFailure, ErrCodeServerError:
		return code == 500 || code == 502 || code == 503 || code == 504
	default:
		return false
	}
}

func backendErrorDetails(err error) (int, string) {
	if err == nil {
		return 500, "transfer backend error"
	}
	var backendErr *BackendError
	if errors.As(err, &backendErr) {
		return backendErr.Code, backendErr.Message
	}
	return 500, "transfer backend error"
}

type LocalStoreBackend struct {
	storeDir string
}

func NewLocalStoreBackend(storeDir string) *LocalStoreBackend {
	return &LocalStoreBackend{
		storeDir: strings.TrimSpace(storeDir),
	}
}

func (b *LocalStoreBackend) Initialize(session *Session) error {
	if err := b.validateSession(session); err != nil {
		return err
	}
	if b.storeDir == "" {
		return newBackendError(501, "local store backend is not configured", nil)
	}
	if err := os.MkdirAll(b.storeDir, 0o700); err != nil {
		return newBackendError(500, "failed to prepare local object store", err)
	}
	return nil
}

func (b *LocalStoreBackend) Upload(session *Session, oid, sourcePath string, expectedSize int64) (int64, error) {
	return b.UploadWithProgress(session, oid, sourcePath, expectedSize, nil)
}

func (b *LocalStoreBackend) UploadWithProgress(session *Session, oid, sourcePath string, expectedSize int64, progress ProgressFunc) (int64, error) {
	if err := b.Initialize(session); err != nil {
		return 0, err
	}

	objectPath := b.objectPath(oid)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o700); err != nil {
		return 0, newBackendError(500, "failed to prepare local object directory", err)
	}
	if _, err := copyFileWithProgress(sourcePath, objectPath, progress); err != nil {
		return 0, newBackendError(500, "failed to persist object in local store", err)
	}

	hash, size, err := calculateFileSHA256(objectPath)
	if err != nil {
		_ = os.Remove(objectPath)
		return 0, newBackendError(500, "failed to verify stored object", err)
	}
	if hash != oid {
		_ = os.Remove(objectPath)
		return 0, newBackendError(500, "stored object hash mismatch", nil)
	}
	if expectedSize > 0 && size != expectedSize {
		_ = os.Remove(objectPath)
		return 0, newBackendError(409, "stored object size does not match transfer request", nil)
	}
	return size, nil
}

func (b *LocalStoreBackend) Download(session *Session, oid string, expectedSize int64) (string, int64, error) {
	return b.DownloadWithProgress(session, oid, expectedSize, nil)
}

func (b *LocalStoreBackend) DownloadWithProgress(session *Session, oid string, _ int64, progress ProgressFunc) (string, int64, error) {
	if err := b.validateSession(session); err != nil {
		return "", 0, err
	}
	if b.storeDir == "" {
		return "", 0, newBackendError(501, "local store backend is not configured", nil)
	}

	objectPath := b.objectPath(oid)
	hash, size, err := calculateFileSHA256(objectPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", 0, newBackendError(404, "object not found in local store", err)
		}
		return "", 0, newBackendError(500, "failed to read object from local store", err)
	}
	if hash != oid {
		return "", 0, newBackendError(500, "stored object hash mismatch", nil)
	}

	tmpFile, err := os.CreateTemp("", "git-lfs-proton-download-*")
	if err != nil {
		return "", 0, newBackendError(500, "failed to create temporary download file", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := copyIntoOpenFileWithProgress(objectPath, tmpFile, progress); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", 0, newBackendError(500, "failed to stage object for download", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, newBackendError(500, "failed to finalize staged download object", err)
	}

	return tmpPath, size, nil
}

func (b *LocalStoreBackend) validateSession(session *Session) error {
	if session == nil || !session.Initialized {
		return newBackendError(500, "session not initialized", nil)
	}
	return nil
}

func (b *LocalStoreBackend) objectPath(oid string) string {
	if len(oid) < 4 {
		return filepath.Join(b.storeDir, oid)
	}
	return filepath.Join(b.storeDir, oid[:2], oid[2:4], oid)
}

// DriveCLIBackend transfers objects through proton-drive-cli bridge
// subprocesses. It requires a saved browser-fork session
// (`proton-drive-cli login`); transfers never log in.
type DriveCLIBackend struct {
	bridge        *BridgeClient
	authenticated bool
}

// NewDriveCLIBackend creates a backend that delegates to proton-drive-cli.
func NewDriveCLIBackend(bridge *BridgeClient) *DriveCLIBackend {
	return &DriveCLIBackend{bridge: bridge}
}

// TerminateBridge kills a running bridge subprocess (see BridgeClient.TerminateActive).
func (b *DriveCLIBackend) TerminateBridge() {
	if b.bridge != nil {
		b.bridge.TerminateActive()
	}
}

func (b *DriveCLIBackend) Initialize(session *Session) error {
	if session == nil || !session.Initialized {
		return newBackendError(500, "session not initialized", nil)
	}
	if b.bridge == nil {
		return newBackendError(500, "drive-cli backend bridge is not configured", nil)
	}

	authState, err := b.bridge.AuthState()
	if err != nil {
		return mapBridgeError(err, "failed to inspect proton drive auth state")
	}
	if err := mapAuthStateForTransfer(authState); err != nil {
		return err
	}

	if err := b.bridge.InitLFSStorage(); err != nil {
		return mapBridgeError(err, "failed to initialize lfs storage")
	}

	b.authenticated = true
	return nil
}

func mapAuthStateForTransfer(state *BridgeAuthStateResponse) error {
	if state == nil {
		return newBackendError(502, "proton drive auth state is unavailable", nil)
	}

	// The bridge reports why a state is not ready in errors[]; carry that
	// into the message so the tray and git-lfs show the cause.
	withCause := func(message string) string {
		if len(state.Errors) == 0 {
			return message
		}
		return message + ": " + strings.Join(state.Errors, "; ")
	}

	switch strings.TrimSpace(state.State) {
	case "ready", "session_expired":
		// `bridge init` refreshes an expired access token before it uses the
		// session (proton-drive-cli src/sdk/client.ts loadUsableSession).
		return nil
	case "needs_key_password":
		return newBackendErrorWithCode(401, withCause("stored browser-fork key password required for this Proton session"), nil, ErrCodeKeyPasswordRequired)
	case "needs_login":
		return newBackendError(401, withCause("no ready Proton Drive session; run proton-drive-cli login before Git LFS transfer"), nil)
	case "session_invalid":
		return newBackendError(401, withCause("Proton Drive session is invalid; run proton-drive-cli login before Git LFS transfer"), nil)
	default:
		return newBackendError(502, withCause("unrecognized Proton Drive auth state"), nil)
	}
}

func (b *DriveCLIBackend) Upload(session *Session, oid, sourcePath string, expectedSize int64) (int64, error) {
	if session == nil || !session.Initialized {
		return 0, newBackendError(500, "session not initialized", nil)
	}
	if !b.authenticated {
		return 0, newBackendError(401, "drive-cli backend is not authenticated", nil)
	}
	if b.bridge == nil {
		return 0, newBackendError(500, "drive-cli backend bridge is not configured", nil)
	}

	exists, err := b.bridge.Exists(oid)
	if err != nil {
		return 0, mapBridgeError(err, "drive-cli existence check failed")
	}
	if exists {
		info, statErr := os.Stat(sourcePath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return 0, newBackendError(404, "upload source file not found", statErr)
			}
			return 0, newBackendError(500, "failed to stat upload source file", statErr)
		}
		return info.Size(), nil
	}

	if err := b.bridge.Upload(oid, sourcePath, expectedSize); err != nil {
		return 0, mapBridgeError(err, "drive-cli upload failed")
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, newBackendError(404, "upload source file not found", err)
		}
		return 0, newBackendError(500, "failed to stat upload source file", err)
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return 0, newBackendError(409, "upload size does not match transfer request", nil)
	}
	return info.Size(), nil
}

func (b *DriveCLIBackend) Download(session *Session, oid string, expectedSize int64) (string, int64, error) {
	if session == nil || !session.Initialized {
		return "", 0, newBackendError(500, "session not initialized", nil)
	}
	if !b.authenticated {
		return "", 0, newBackendError(401, "drive-cli backend is not authenticated", nil)
	}
	if b.bridge == nil {
		return "", 0, newBackendError(500, "drive-cli backend bridge is not configured", nil)
	}

	tmpFile, err := os.CreateTemp("", "git-lfs-proton-download-*")
	if err != nil {
		return "", 0, newBackendError(500, "failed to create temporary download file", err)
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, newBackendError(500, "failed to create temporary download file", err)
	}

	if err := b.bridge.Download(oid, tmpPath, expectedSize); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, mapBridgeError(err, "drive-cli download failed")
	}
	// The bridge recreates the staging file on retry; keep the plaintext
	// owner-readable only (CWE-732) regardless of the subprocess umask.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, newBackendError(500, "failed to protect downloaded object", err)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		if errors.Is(err, os.ErrNotExist) {
			return "", 0, newBackendError(500, "drive-cli backend did not materialize download output", err)
		}
		return "", 0, newBackendError(500, "failed to stat downloaded object", err)
	}

	return tmpPath, info.Size(), nil
}

// mapBridgeError maps an envelope error (*BridgeError) by its errorCode and a
// transport failure (spawn, timeout, malformed output) by its message.
func mapBridgeError(err error, fallbackMessage string) error {
	if err == nil {
		return nil
	}

	var bridgeErr *BridgeError
	if errors.As(err, &bridgeErr) {
		return mapStructuredBridgeError(bridgeErr, fallbackMessage, err)
	}
	var timeoutErr *BridgeTimeoutError
	if errors.As(err, &timeoutErr) {
		return newBackendErrorWithCode(504, fmt.Sprintf(
			"%s; raise --bridge-timeout or PROTON_LFS_BRIDGE_TIMEOUT for large objects or slow links",
			timeoutErr.Error()), err, ErrCodeNetworkFailure)
	}

	var startErr *BridgeStartError
	if errors.As(err, &startErr) {
		return newBackendErrorWithCode(400, "proton-drive-cli could not be started; check --drive-cli-bin, PROTON_DRIVE_CLI_BIN and NODE_BIN", err, ErrCodeInvalidRequest)
	}

	// Transport failures carry free text (spawn errors, sanitized stderr);
	// only words are matched, never bare status digits that an OID can contain.
	msg := strings.ToLower(strings.TrimSpace(err.Error()))

	switch {
	case strings.Contains(msg, "key password"):
		return newBackendErrorWithCode(401, "stored browser-fork key password required for this Proton session", err, ErrCodeKeyPasswordRequired)
	case strings.Contains(msg, "invalid or expired session"),
		strings.Contains(msg, "unauthorized"):
		return newBackendError(401, "session is invalid or expired", err)
	case strings.Contains(msg, "sufficient scope"):
		return newBackendErrorWithCode(403, "Proton rejected this app/session authorization scope", err, ErrCodeInsufficientScope)
	case strings.Contains(msg, "not found"):
		return newBackendError(404, "object not found in drive backend", err)
	case strings.Contains(msg, "captcha"):
		return newBackendError(407, "captcha verification required — run: proton-drive-cli login", err)
	case strings.Contains(msg, "rate limit"):
		return newBackendError(429, "rate limited by proton api — wait and retry", err)
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "dial tcp"):
		return newBackendError(503, "drive service is unavailable", err)
	default:
		return newBackendError(502, fallbackMessage, err)
	}
}

// bridgeErrorDetails is the part of schemas/bridge/v1/error-details.schema.json
// the adapter maps; errorCode is always present.
type bridgeErrorDetails struct {
	ErrorCode string `json:"errorCode"`
}

func bridgeStatusOrDefault(bridgeErr *BridgeError, fallback int) int {
	if bridgeErr != nil && bridgeErr.Code > 0 {
		return bridgeErr.Code
	}
	return fallback
}

func mapStructuredBridgeError(bridgeErr *BridgeError, fallbackMessage string, original error) error {
	if bridgeErr == nil {
		return newBackendError(502, fallbackMessage, original)
	}

	msg := strings.TrimSpace(bridgeErr.Message)
	if msg == "" {
		msg = fallbackMessage
	}

	var details bridgeErrorDetails
	if bridgeErr.Details != "" {
		_ = json.Unmarshal([]byte(bridgeErr.Details), &details)
	}
	errorCode := strings.ToUpper(strings.TrimSpace(details.ErrorCode))
	if ok, mapped := mapBridgeErrorCode(errorCode, bridgeErr, msg, fallbackMessage, original); ok {
		return mapped
	}

	switch bridgeErr.Code {
	case 401:
		return newBackendError(401, "session is invalid or expired", original)
	case 404:
		return newBackendError(404, "object not found in drive backend", original)
	case 407:
		return newBackendError(407, "captcha verification required — run: proton-drive-cli login", original)
	case 429:
		return newBackendError(429, "rate limited by proton api — wait and retry", original)
	case 403:
		return newBackendErrorWithCode(403, "permission denied by drive backend", original, ErrCodePermissionDenied)
	case 503:
		return newBackendError(503, "drive service is unavailable", original)
	default:
		if bridgeErr.Code >= 500 {
			return newBackendError(bridgeErr.Code, fallbackMessage, original)
		}
		if bridgeErr.Code >= 400 {
			return newBackendError(bridgeErr.Code, msg, original)
		}
		return newBackendError(502, fallbackMessage, original)
	}
}

// mapBridgeErrorCode maps a structured bridge error code (the keys of
// schemas/bridge/v1/error-code-map.json) to a BackendError. The HTTP status
// reported by the bridge is preserved when present.
func mapBridgeErrorCode(errorCode string, bridgeErr *BridgeError, msg, fallbackMessage string, original error) (bool, error) {
	switch errorCode {
	case "AUTH_FAILED", "SESSION_EXPIRED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 401), "session is invalid or expired", original, ErrCodeAuthRequired)
	case "KEY_PASSWORD_REQUIRED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 401), "stored browser-fork key password required for this Proton session", original, ErrCodeKeyPasswordRequired)
	case "CAPTCHA_REQUIRED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 407), "captcha verification required — run: proton-drive-cli login", original, ErrCodeCaptchaRequired)
	case "RATE_LIMITED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 429), "rate limited by proton api — wait and retry", original, ErrCodeRateLimited)
	case "INSUFFICIENT_SCOPE":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 403), "Proton rejected this app/session authorization scope", original, ErrCodeInsufficientScope)
	case "NOT_FOUND", "FILE_NOT_FOUND", "PATH_NOT_FOUND":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 404), "object not found in drive backend", original, ErrCodeNotFound)
	case "PERMISSION_DENIED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 403), "permission denied by drive backend", original, ErrCodePermissionDenied)
	case "NETWORK_ERROR", "TIMEOUT", "CONNECTION_REFUSED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 502), "drive service is unavailable", original, ErrCodeNetworkFailure)
	case "FILE_TOO_LARGE", "INVALID_FILE", "INVALID_PATH", "NOT_A_FOLDER", "VALIDATION_ERROR", "OPERATION_CANCELLED":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 400), msg, original, ErrCodeInvalidRequest)
	case "QUOTA_EXCEEDED", "DISK_FULL":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 507), msg, original, ErrCodeServerError)
	case "ENCRYPTION_FAILED", "DECRYPTION_FAILED":
		// Local cryptographic failures: status 500 by contract, never retryable.
		err := newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 500), msg, original, ErrCodeServerError)
		var backendErr *BackendError
		if errors.As(err, &backendErr) {
			backendErr.Retryable = false
			backendErr.Temporary = false
		}
		return true, err
	case "API_ERROR", "UPLOAD_FAILED", "DOWNLOAD_FAILED", "UNKNOWN_ERROR":
		return true, newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 502), fallbackMessage, original, ErrCodeServerError)
	default:
		return false, nil
	}
}
