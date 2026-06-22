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
	Download(session *Session, oid string) (string, int64, error)
}

// ErrorCode is a machine-readable error classification for structured error handling.
type ErrorCode string

const (
	ErrCodeNetworkFailure       ErrorCode = "network_failure"
	ErrCodeAuthRequired         ErrorCode = "auth_required"
	ErrCodeRateLimited          ErrorCode = "rate_limited"
	ErrCodeCaptchaRequired      ErrorCode = "captcha_required"
	ErrCodeTwoFactorRequired    ErrorCode = "two_factor_required"
	ErrCodeDataPasswordRequired ErrorCode = "data_password_required"
	ErrCodeKeyPasswordRequired  ErrorCode = "key_password_required"
	ErrCodeKeyUnlockFailed      ErrorCode = "key_unlock_failed"
	ErrCodeNotFound             ErrorCode = "not_found"
	ErrCodePermissionDenied     ErrorCode = "permission_denied"
	ErrCodeServerError          ErrorCode = "server_error"
	ErrCodeInvalidRequest       ErrorCode = "invalid_request"
	ErrCodeUnknown              ErrorCode = "unknown"
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
	return &BackendError{
		Code:      code,
		Message:   message,
		Err:       err,
		ErrorCode: errorCode,
		Retryable: isRetryableCode(code),
		Temporary: isTemporaryCode(code),
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

// isRetryableCode determines if an HTTP status code indicates a retryable error
func isRetryableCode(code int) bool {
	// 5xx server errors are retryable, except when rate-limited (429)
	return code >= 500 && code < 600
}

// isTemporaryCode determines if an HTTP status code indicates a temporary error
func isTemporaryCode(code int) bool {
	// 503 (Service Unavailable) and 5xx are typically temporary
	return code == 503 || (code >= 500 && code < 600)
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

// OperationCredentials holds per-request credential provider info sent
// alongside bridge commands. The provider name is passed to proton-drive-cli
// which resolves credentials locally (git-credential, pass-cli, etc.).
type OperationCredentials struct {
	CredentialProvider     string
	DataCredentialProvider string
	DataCredentialHost     string
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
	if err := b.Initialize(session); err != nil {
		return 0, err
	}

	objectPath := b.objectPath(oid)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o700); err != nil {
		return 0, newBackendError(500, "failed to prepare local object directory", err)
	}
	if err := copyFile(sourcePath, objectPath); err != nil {
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

func (b *LocalStoreBackend) Download(session *Session, oid string) (string, int64, error) {
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

	if err := copyIntoOpenFile(objectPath, tmpFile); err != nil {
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

// DriveCLIBackend communicates directly with proton-drive-cli via subprocess.
// Credential resolution is fully delegated to proton-drive-cli — the Go adapter
// only passes the provider name (git-credential, pass-cli, etc.).
type DriveCLIBackend struct {
	bridge                 *BridgeClient
	credentialProvider     string
	dataCredentialProvider string
	dataCredentialHost     string
	authenticated          bool
}

type DriveCLIBackendOptions struct {
	DataCredentialProvider string
	DataCredentialHost     string
}

// NewDriveCLIBackend creates a backend that delegates to proton-drive-cli.
// The credentialProvider name is forwarded to proton-drive-cli which resolves
// credentials locally.
func NewDriveCLIBackend(bridge *BridgeClient, credentialProvider string, opts ...DriveCLIBackendOptions) *DriveCLIBackend {
	var options DriveCLIBackendOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	return &DriveCLIBackend{
		bridge:                 bridge,
		credentialProvider:     credentialProvider,
		dataCredentialProvider: options.DataCredentialProvider,
		dataCredentialHost:     options.DataCredentialHost,
	}
}

func (b *DriveCLIBackend) operationCredentials() OperationCredentials {
	return OperationCredentials{
		CredentialProvider:     b.credentialProvider,
		DataCredentialProvider: b.dataCredentialProvider,
		DataCredentialHost:     b.dataCredentialHost,
	}
}

func (b *DriveCLIBackend) Initialize(session *Session) error {
	if session == nil || !session.Initialized {
		return newBackendError(500, "session not initialized", nil)
	}
	if b.bridge == nil {
		return newBackendError(500, "drive-cli backend bridge is not configured", nil)
	}

	creds := b.operationCredentials()

	authState, err := b.bridge.AuthState(creds)
	if err != nil {
		return mapBridgeError(err, "failed to inspect proton drive auth state")
	}
	if err := mapAuthStateForTransfer(authState); err != nil {
		return err
	}

	if err := b.bridge.InitLFSStorage(creds); err != nil {
		return mapBridgeError(err, "failed to initialize lfs storage")
	}

	b.authenticated = true
	session.Token = "direct-bridge"
	return nil
}

func mapAuthStateForTransfer(state *BridgeAuthStateResponse) error {
	if state == nil {
		return newBackendError(502, "proton drive auth state is unavailable", nil)
	}

	switch strings.TrimSpace(state.State) {
	case "ready":
		return nil
	case "needs_data_password":
		return newBackendErrorWithCode(401, "mailbox/data password required for this Proton account", nil, ErrCodeDataPasswordRequired)
	case "needs_key_password":
		return newBackendErrorWithCode(401, "stored browser-fork key password required for this Proton session", nil, ErrCodeKeyPasswordRequired)
	case "login_available", "needs_login":
		return newBackendError(401, "no ready Proton Drive session; run proton-drive login before Git LFS transfer", nil)
	case "session_expired":
		return newBackendError(401, "Proton Drive session is expired; refresh or login before Git LFS transfer", nil)
	case "session_invalid":
		return newBackendError(401, "Proton Drive session is invalid; run proton-drive login before Git LFS transfer", nil)
	case "configuration_error":
		return newBackendError(400, "Proton Drive auth configuration error", nil)
	default:
		return newBackendError(502, "unrecognized Proton Drive auth state", nil)
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

	// Dedup: skip upload if OID already exists in remote storage
	exists, err := b.bridge.Exists(b.operationCredentials(), oid)
	if err == nil && exists {
		info, statErr := os.Stat(sourcePath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return 0, newBackendError(404, "upload source file not found", statErr)
			}
			return 0, newBackendError(500, "failed to stat upload source file", statErr)
		}
		return info.Size(), nil
	}

	if err := b.bridge.Upload(b.operationCredentials(), oid, sourcePath); err != nil {
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

func (b *DriveCLIBackend) Download(session *Session, oid string) (string, int64, error) {
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

	if err := b.bridge.Download(b.operationCredentials(), oid, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, mapBridgeError(err, "drive-cli download failed")
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

// mapBridgeError converts bridge subprocess errors into BackendErrors with
// appropriate HTTP-style status codes, matching the logic from the old
// mapSDKError function.
func mapBridgeError(err error, fallbackMessage string) error {
	if err == nil {
		return nil
	}

	var bridgeErr *BridgeError
	if errors.As(err, &bridgeErr) {
		return mapStructuredBridgeError(bridgeErr, fallbackMessage, err)
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))

	// Parse [code] prefix from bridge error format
	if strings.HasPrefix(msg, "[") {
		if idx := strings.Index(msg, "]"); idx > 1 {
			codeStr := msg[1:idx]
			rest := strings.TrimSpace(msg[idx+1:])
			switch codeStr {
			case "401":
				return newBackendError(401, "session is invalid or expired", err)
			case "404":
				return newBackendError(404, "object not found in drive backend", err)
			case "407":
				return newBackendError(407, "captcha verification required — run: proton-drive login", err)
			case "429":
				return newBackendError(429, "rate limited by proton api — wait and retry", err)
			case "503":
				return newBackendError(503, "drive service is unavailable", err)
			default:
				if rest != "" {
					return newBackendError(502, fallbackMessage, err)
				}
			}
		}
	}

	switch {
	case strings.Contains(msg, "two-factor"),
		strings.Contains(msg, "2fa"),
		strings.Contains(msg, "fido2"):
		return newBackendErrorWithCode(401, "two-factor authentication required", err, ErrCodeTwoFactorRequired)
	case strings.Contains(msg, "key password"):
		return newBackendErrorWithCode(401, "stored browser-fork key password required for this Proton session", err, ErrCodeKeyPasswordRequired)
	case strings.Contains(msg, "mailbox/data password"),
		strings.Contains(msg, "data password"),
		strings.Contains(msg, "key decryption"):
		return newBackendErrorWithCode(401, "mailbox/data password required for this Proton account", err, ErrCodeDataPasswordRequired)
	case strings.Contains(msg, "failed to decrypt"),
		strings.Contains(msg, "decrypt any keys"):
		return newBackendErrorWithCode(401, "mailbox/data password could not unlock Proton keys", err, ErrCodeKeyUnlockFailed)
	case strings.Contains(msg, "invalid or expired session"),
		strings.Contains(msg, "unauthorized"),
		strings.Contains(msg, "401"):
		return newBackendError(401, "session is invalid or expired", err)
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "404"):
		return newBackendError(404, "object not found in drive backend", err)
	case strings.Contains(msg, "captcha"):
		return newBackendError(407, "captcha verification required — run: proton-drive login", err)
	case strings.Contains(msg, "rate limit"):
		return newBackendError(429, "rate limited by proton api — wait and retry", err)
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "dial tcp"):
		return newBackendError(503, "drive service is unavailable", err)
	case strings.Contains(msg, "concurrency limit"):
		return newBackendError(503, "bridge concurrency limit reached", err)
	default:
		return newBackendError(502, fallbackMessage, err)
	}
}

type bridgeErrorDetails struct {
	ErrorCode      string `json:"errorCode"`
	TwoFactorType  string `json:"twoFactorType"`
	TOTPAllowed    bool   `json:"totpAllowed"`
	FIDO2Available bool   `json:"fido2Available"`
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
	lowerMsg := strings.ToLower(msg)

	var details bridgeErrorDetails
	if bridgeErr.Details != "" {
		_ = json.Unmarshal([]byte(bridgeErr.Details), &details)
	}
	errorCode := strings.ToUpper(strings.TrimSpace(details.ErrorCode))

	switch {
	case errorCode == "AUTH_FAILED" || errorCode == "INVALID_CREDENTIALS" || errorCode == "SESSION_EXPIRED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 401), "session is invalid or expired", original, ErrCodeAuthRequired)
	case errorCode == "TWO_FACTOR_REQUIRED" || strings.Contains(lowerMsg, "two-factor") || strings.Contains(lowerMsg, "2fa") || strings.Contains(lowerMsg, "fido2"):
		message := "two-factor authentication required"
		if details.TwoFactorType == "fido2" {
			message = "FIDO2 two-factor authentication required"
		}
		return newBackendErrorWithCode(401, message, original, ErrCodeTwoFactorRequired)
	case errorCode == "KEY_PASSWORD_REQUIRED" || strings.Contains(lowerMsg, "key password"):
		return newBackendErrorWithCode(401, "stored browser-fork key password required for this Proton session", original, ErrCodeKeyPasswordRequired)
	case errorCode == "DATA_PASSWORD_REQUIRED" || strings.Contains(lowerMsg, "mailbox/data password") || strings.Contains(lowerMsg, "data password") || strings.Contains(lowerMsg, "key decryption"):
		return newBackendErrorWithCode(401, "mailbox/data password required for this Proton account", original, ErrCodeDataPasswordRequired)
	case strings.Contains(lowerMsg, "failed to decrypt") || strings.Contains(lowerMsg, "decrypt any keys"):
		return newBackendErrorWithCode(401, "mailbox/data password could not unlock Proton keys", original, ErrCodeKeyUnlockFailed)
	case errorCode == "CAPTCHA_REQUIRED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 407), "captcha verification required — run: proton-drive login", original, ErrCodeCaptchaRequired)
	case errorCode == "RATE_LIMITED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 429), "rate limited by proton api — wait and retry", original, ErrCodeRateLimited)
	case errorCode == "NOT_FOUND" || errorCode == "FILE_NOT_FOUND" || errorCode == "PATH_NOT_FOUND":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 404), "object not found in drive backend", original, ErrCodeNotFound)
	case errorCode == "PERMISSION_DENIED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 403), "permission denied by drive backend", original, ErrCodePermissionDenied)
	case errorCode == "NETWORK_ERROR" || errorCode == "TIMEOUT" || errorCode == "CONNECTION_REFUSED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 502), "drive service is unavailable", original, ErrCodeNetworkFailure)
	case errorCode == "FILE_TOO_LARGE" || errorCode == "INVALID_FILE" || errorCode == "INVALID_PATH" || errorCode == "NOT_A_FOLDER" || errorCode == "VALIDATION_ERROR" || errorCode == "OPERATION_CANCELLED":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 400), msg, original, ErrCodeInvalidRequest)
	case errorCode == "API_ERROR" || errorCode == "QUOTA_EXCEEDED" || errorCode == "DISK_FULL" || errorCode == "UPLOAD_FAILED" || errorCode == "DOWNLOAD_FAILED" || errorCode == "ENCRYPTION_FAILED" || errorCode == "DECRYPTION_FAILED" || errorCode == "UNKNOWN_ERROR":
		return newBackendErrorWithCode(bridgeStatusOrDefault(bridgeErr, 502), fallbackMessage, original, ErrCodeServerError)
	}

	switch bridgeErr.Code {
	case 401:
		return newBackendError(401, "session is invalid or expired", original)
	case 404:
		return newBackendError(404, "object not found in drive backend", original)
	case 407:
		return newBackendError(407, "captcha verification required — run: proton-drive login", original)
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
