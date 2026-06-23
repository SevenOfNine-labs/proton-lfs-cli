package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriveCLIBackendRoundTrip(t *testing.T) {
	payload := []byte("drive-cli-roundtrip")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload source: %v", err)
	}

	// Use exists=false so upload doesn't skip via dedup
	bc := helperBridgeClient(t, "MOCK_BRIDGE_EXISTS_RESULT=false", "MOCK_BRIDGE_DOWNLOAD_CONTENT="+string(payload))
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	// Initialize (offline auth-state + init)
	if err := backend.Initialize(session); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if session.Token != "direct-bridge" {
		t.Fatalf("expected sentinel token, got %q", session.Token)
	}
	if !backend.authenticated {
		t.Fatal("expected authenticated=true")
	}

	// Upload
	uploadedSize, err := backend.Upload(session, oid, uploadPath, int64(len(payload)))
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}
	if uploadedSize != int64(len(payload)) {
		t.Fatalf("unexpected upload size: %d", uploadedSize)
	}

	// Download
	downloadPath, downloadedSize, err := backend.Download(session, oid)
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	defer os.Remove(downloadPath)

	if downloadedSize != int64(len(payload)) {
		t.Fatalf("unexpected download size: %d", downloadedSize)
	}
	downloadedBytes, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(downloadedBytes) != string(payload) {
		t.Fatal("downloaded bytes mismatch")
	}
}

func TestDriveCLIBackendInitializeWithEmptyProvider(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	// With empty provider, a ready local session can initialize without
	// allowing a full login attempt from the transfer path.
	err := backend.Initialize(session)
	if err != nil {
		t.Fatalf("Initialize with empty provider should succeed when ready: %v", err)
	}
}

func TestDriveCLIBackendInitializeUsesOfflineGateBeforeInit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "bridge-commands.log")
	bc := helperBridgeClient(t, "MOCK_BRIDGE_COMMAND_LOG="+logPath)
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	if err := backend.Initialize(session); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	commands := readLoggedBridgeCommands(t, logPath)
	assertBridgeCommands(t, commands, "auth-state", "init")

	if _, ok := commands[0].Request["allowLogin"]; ok {
		t.Fatalf("auth-state request should not include allowLogin, got %v", commands[0].Request)
	}
	if _, ok := commands[1].Request["allowLogin"]; ok {
		t.Fatalf("init request should not include allowLogin, got %v", commands[1].Request)
	}
	if _, ok := commands[1].Request["credentialProvider"]; ok {
		t.Fatalf("init request should not include credentialProvider, got %v", commands[1].Request)
	}
}

func TestDriveCLIBackendInitializeBlocksAllNonReadyStatesBeforeInitOrAuth(t *testing.T) {
	cases := []struct {
		state     string
		code      int
		errorCode ErrorCode
	}{
		{state: "needs_login", code: 401, errorCode: ErrCodeAuthRequired},
		{state: "needs_data_password", code: 401, errorCode: ErrCodeDataPasswordRequired},
		{state: "needs_key_password", code: 401, errorCode: ErrCodeKeyPasswordRequired},
		{state: "session_expired", code: 401, errorCode: ErrCodeAuthRequired},
		{state: "session_invalid", code: 401, errorCode: ErrCodeAuthRequired},
		{state: "configuration_error", code: 400, errorCode: ErrCodeInvalidRequest},
	}

	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "bridge-commands.log")
			bc := helperBridgeClient(t,
				"MOCK_BRIDGE_AUTH_STATE="+tc.state,
				"MOCK_BRIDGE_COMMAND_LOG="+logPath,
			)
			backend := NewDriveCLIBackend(bc)
			session := &Session{Initialized: true, CreatedAt: time.Now()}

			err := backend.Initialize(session)
			var backendErr *BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("expected BackendError, got %T %v", err, err)
			}
			if backendErr.Code != tc.code || backendErr.ErrorCode != tc.errorCode {
				t.Fatalf("state %s mapped to code=%d errorCode=%s, want code=%d errorCode=%s",
					tc.state, backendErr.Code, backendErr.ErrorCode, tc.code, tc.errorCode)
			}
			if backend.authenticated {
				t.Fatalf("state %s must not mark backend authenticated", tc.state)
			}

			commands := readLoggedBridgeCommands(t, logPath)
			assertBridgeCommands(t, commands, "auth-state")
		})
	}
}

func TestDriveCLIBackendUploadMapsNotFoundError(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_EXISTS_RESULT=false",
		"MOCK_BRIDGE_ERROR=not found",
		"MOCK_BRIDGE_ERROR_CODE=404",
	)
	backend := NewDriveCLIBackend(bc)
	backend.authenticated = true

	session := &Session{Initialized: true, Token: "direct-bridge"}

	_, err := backend.Upload(session, validOID, "/tmp/does-not-exist", 0)
	code, _ := backendErrorDetails(err)
	if code != 404 {
		t.Fatalf("expected mapped not-found code 404, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendDownloadMapsAuthErrorAndCleansOutput(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_ERROR=unauthorized",
		"MOCK_BRIDGE_ERROR_CODE=401",
	)
	backend := NewDriveCLIBackend(bc)
	backend.authenticated = true

	session := &Session{Initialized: true, Token: "direct-bridge"}

	_, _, err := backend.Download(session, validOID)
	code, _ := backendErrorDetails(err)
	if code != 401 {
		t.Fatalf("expected mapped auth code 401, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendUploadDedup(t *testing.T) {
	payload := []byte("dedup-test")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	// Mock bridge says exists=true, so upload should be skipped
	bc := helperBridgeClient(t)
	backend := NewDriveCLIBackend(bc)
	backend.authenticated = true

	session := &Session{Initialized: true, Token: "direct-bridge"}

	size, err := backend.Upload(session, oid, uploadPath, int64(len(payload)))
	if err != nil {
		t.Fatalf("Upload should succeed with dedup: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("unexpected size: %d", size)
	}
}

func TestDriveCLIBackendUploadFailsClosedWhenExistsCheckFails(t *testing.T) {
	payload := []byte("exists-check-failure")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "bridge-commands.log")
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_COMMAND_LOG="+logPath,
		"MOCK_BRIDGE_ERROR_COMMAND=exists",
		"MOCK_BRIDGE_ERROR=drive service is unavailable",
		"MOCK_BRIDGE_ERROR_CODE=503",
	)
	backend := NewDriveCLIBackend(bc)
	backend.authenticated = true

	session := &Session{Initialized: true, Token: "direct-bridge"}
	_, err := backend.Upload(session, oid, uploadPath, int64(len(payload)))

	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected BackendError, got %T %v", err, err)
	}
	if backendErr.Code != 503 || backendErr.ErrorCode != ErrCodeServerError {
		t.Fatalf("unexpected backend error classification: %+v", backendErr)
	}
	if !backendErr.Retryable || !backendErr.Temporary {
		t.Fatalf("exists-check outage should be retryable and temporary: %+v", backendErr)
	}

	commands := readLoggedBridgeCommands(t, logPath)
	assertBridgeCommands(t, commands, "exists")
}

func TestDriveCLIBackendGitCredentialMode(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge: bc,
	}

	session := &Session{Initialized: true, CreatedAt: time.Now()}
	if err := backend.Initialize(session); err != nil {
		t.Fatalf("Initialize with git-credential failed: %v", err)
	}
	if session.Token != "direct-bridge" {
		t.Fatalf("expected sentinel token, got %q", session.Token)
	}
}

func TestMapBridgeError(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantCode int
	}{
		{"401 prefix", "[401] unauthorized", 401},
		{"404 prefix", "[404] not found", 404},
		{"503 prefix", "[503] service unavailable", 503},
		{"text 401", "unauthorized access", 401},
		{"text 404", "object not found", 404},
		{"text timeout", "request timed out", 503},
		{"text connection refused", "connection refused", 503},
		{"407 prefix", "[407] captcha verification required", 407},
		{"429 prefix", "[429] rate limited", 429},
		{"text captcha", "captcha verification required", 407},
		{"text rate limit", "rate limit exceeded", 429},
		{"text two factor", "Two-factor authentication code required", 401},
		{"text data password", "Mailbox/data password required for this two-password Proton account", 401},
		{"text key password", "Browser-fork session is missing its stored key password", 401},
		{"text key unlock", "Failed to decrypt any keys", 401},
		{"concurrency limit", "bridge concurrency limit reached (10)", 503},
		{"unknown error", "something unexpected", 502},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapBridgeError(errors.New(tc.input), "fallback")
			code, _ := backendErrorDetails(err)
			if code != tc.wantCode {
				t.Fatalf("expected code %d, got %d for input %q", tc.wantCode, code, tc.input)
			}
		})
	}
}

func TestMapStructuredBridgeErrorAuthStates(t *testing.T) {
	cases := []struct {
		name      string
		err       *BridgeError
		wantCode  int
		wantClass ErrorCode
	}{
		{
			name: "totp required",
			err: &BridgeError{
				Code:    401,
				Message: "Two-factor authentication code required",
				Details: `{"errorCode":"TWO_FACTOR_REQUIRED","twoFactorType":"totp","totpAllowed":true}`,
			},
			wantCode:  401,
			wantClass: ErrCodeTwoFactorRequired,
		},
		{
			name: "fido2 required",
			err: &BridgeError{
				Code:    401,
				Message: "FIDO2 two-factor authentication is required",
				Details: `{"errorCode":"TWO_FACTOR_REQUIRED","twoFactorType":"fido2","fido2Available":true}`,
			},
			wantCode:  401,
			wantClass: ErrCodeTwoFactorRequired,
		},
		{
			name: "data password required",
			err: &BridgeError{
				Code:    401,
				Message: "Mailbox/data password required for this two-password Proton account",
				Details: `{"errorCode":"DATA_PASSWORD_REQUIRED","passwordMode":2}`,
			},
			wantCode:  401,
			wantClass: ErrCodeDataPasswordRequired,
		},
		{
			name: "key password required",
			err: &BridgeError{
				Code:    401,
				Message: "Browser-fork session is missing its stored key password",
				Details: `{"errorCode":"KEY_PASSWORD_REQUIRED"}`,
			},
			wantCode:  401,
			wantClass: ErrCodeKeyPasswordRequired,
		},
		{
			name: "key unlock failed",
			err: &BridgeError{
				Code:    500,
				Message: "Failed to decrypt any keys",
			},
			wantCode:  401,
			wantClass: ErrCodeKeyUnlockFailed,
		},
		{
			name: "insufficient scope",
			err: &BridgeError{
				Code:    403,
				Message: "API Error (9101): Access token does not have sufficient scope",
				Details: `{"errorCode":"INSUFFICIENT_SCOPE","protonCode":9101}`,
			},
			wantCode:  403,
			wantClass: ErrCodeInsufficientScope,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapBridgeError(tc.err, "fallback")
			var backendErr *BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("expected BackendError, got %T", err)
			}
			if backendErr.Code != tc.wantCode {
				t.Fatalf("expected code %d, got %d (%v)", tc.wantCode, backendErr.Code, err)
			}
			if backendErr.ErrorCode != tc.wantClass {
				t.Fatalf("expected class %s, got %s", tc.wantClass, backendErr.ErrorCode)
			}
			if backendErr.Retryable || backendErr.Temporary {
				t.Fatalf("auth state should not be retryable/temporary: %+v", backendErr)
			}
		})
	}
}

func TestMapBridgeErrorNil(t *testing.T) {
	if err := mapBridgeError(nil, "fallback"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestDriveCLIBackendUploadNotAuthenticated(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := NewDriveCLIBackend(bc)
	// NOT authenticated

	session := &Session{Initialized: true, Token: "direct-bridge"}
	_, err := backend.Upload(session, validOID, "/tmp/test", 0)
	code, _ := backendErrorDetails(err)
	if code != 401 {
		t.Fatalf("expected 401, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendDownloadNotAuthenticated(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := NewDriveCLIBackend(bc)
	// NOT authenticated

	session := &Session{Initialized: true, Token: "direct-bridge"}
	_, _, err := backend.Download(session, validOID)
	code, _ := backendErrorDetails(err)
	if code != 401 {
		t.Fatalf("expected 401, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendInitializeCaptchaError(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_ERROR=captcha verification required",
		"MOCK_BRIDGE_ERROR_CODE=407",
	)
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	code, _ := backendErrorDetails(err)
	if code != 407 {
		t.Fatalf("expected 407, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendInitializeRateLimitError(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_ERROR=rate limited by proton api",
		"MOCK_BRIDGE_ERROR_CODE=429",
	)
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	code, _ := backendErrorDetails(err)
	if code != 429 {
		t.Fatalf("expected 429, got %d (%v)", code, err)
	}
}

func TestDriveCLIBackendInitializeBlocksNeedsLoginState(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_AUTH_STATE=needs_login")
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	err := backend.Initialize(session)
	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected BackendError, got %T", err)
	}
	if backendErr.Code != 401 || backendErr.ErrorCode != ErrCodeAuthRequired {
		t.Fatalf("expected auth_required 401, got %+v", backendErr)
	}
	if backend.authenticated {
		t.Fatal("backend must not authenticate when offline auth-state is not ready")
	}
}

func TestDriveCLIBackendInitializeMapsDataPasswordState(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_AUTH_STATE=needs_data_password")
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	err := backend.Initialize(session)
	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected BackendError, got %T", err)
	}
	if backendErr.Code != 401 || backendErr.ErrorCode != ErrCodeDataPasswordRequired {
		t.Fatalf("expected data_password_required 401, got %+v", backendErr)
	}
}

func TestDriveCLIBackendInitializeMapsKeyPasswordState(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_AUTH_STATE=needs_key_password")
	backend := NewDriveCLIBackend(bc)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	err := backend.Initialize(session)
	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected BackendError, got %T", err)
	}
	if backendErr.Code != 401 || backendErr.ErrorCode != ErrCodeKeyPasswordRequired {
		t.Fatalf("expected key_password_required 401, got %+v", backendErr)
	}
	if backend.authenticated {
		t.Fatal("backend must not authenticate when key-password auth-state is not ready")
	}
}

// --- BackendError Tests ---

func TestBackendErrorError(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var e *BackendError
		if e.Error() != "" {
			t.Fatalf("expected empty string for nil, got %q", e.Error())
		}
	})
	t.Run("without inner error", func(t *testing.T) {
		e := &BackendError{Code: 404, Message: "not found"}
		if e.Error() != "not found" {
			t.Fatalf("expected 'not found', got %q", e.Error())
		}
	})
	t.Run("with inner error", func(t *testing.T) {
		inner := errors.New("disk full")
		e := &BackendError{Code: 500, Message: "write failed", Err: inner}
		if !strings.Contains(e.Error(), "write failed") || !strings.Contains(e.Error(), "disk full") {
			t.Fatalf("expected composite message, got %q", e.Error())
		}
	})
}

func TestBackendErrorUnwrap(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var e *BackendError
		if e.Unwrap() != nil {
			t.Fatal("expected nil for nil receiver")
		}
	})
	t.Run("without inner", func(t *testing.T) {
		e := &BackendError{Code: 404, Message: "not found"}
		if e.Unwrap() != nil {
			t.Fatal("expected nil when no inner error")
		}
	})
	t.Run("with inner", func(t *testing.T) {
		inner := errors.New("root cause")
		e := &BackendError{Code: 500, Message: "wrapped", Err: inner}
		if e.Unwrap() != inner {
			t.Fatal("expected inner error from Unwrap")
		}
	})
}

func TestBackendErrorDetailsNilError(t *testing.T) {
	code, msg := backendErrorDetails(nil)
	if code != 500 {
		t.Fatalf("expected 500, got %d", code)
	}
	if msg != "transfer backend error" {
		t.Fatalf("expected fallback message, got %q", msg)
	}
}

func TestBackendErrorDetailsNonBackendError(t *testing.T) {
	code, msg := backendErrorDetails(errors.New("plain error"))
	if code != 500 {
		t.Fatalf("expected 500, got %d", code)
	}
	if msg != "transfer backend error" {
		t.Fatalf("expected fallback message, got %q", msg)
	}
}

func TestBackendErrorDetailsWithBackendError(t *testing.T) {
	err := &BackendError{Code: 404, Message: "object missing"}
	code, msg := backendErrorDetails(err)
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
	if msg != "object missing" {
		t.Fatalf("expected 'object missing', got %q", msg)
	}
}

func TestLocalStoreBackendValidateSession(t *testing.T) {
	b := NewLocalStoreBackend(t.TempDir())
	t.Run("nil session", func(t *testing.T) {
		err := b.Initialize(nil)
		if err == nil {
			t.Fatal("expected error for nil session")
		}
		code, _ := backendErrorDetails(err)
		if code != 500 {
			t.Fatalf("expected 500, got %d", code)
		}
	})
	t.Run("uninitialized session", func(t *testing.T) {
		err := b.Initialize(&Session{Initialized: false})
		if err == nil {
			t.Fatal("expected error for uninitialized session")
		}
	})
	t.Run("valid session", func(t *testing.T) {
		err := b.Initialize(&Session{Initialized: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocalStoreBackendObjectPath(t *testing.T) {
	b := NewLocalStoreBackend("/store")
	t.Run("2-level prefix", func(t *testing.T) {
		path := b.objectPath(validOID)
		expected := filepath.Join("/store", validOID[:2], validOID[2:4], validOID)
		if path != expected {
			t.Fatalf("expected %q, got %q", expected, path)
		}
	})
	t.Run("short OID fallback", func(t *testing.T) {
		path := b.objectPath("abc")
		expected := filepath.Join("/store", "abc")
		if path != expected {
			t.Fatalf("expected %q, got %q", expected, path)
		}
	})
}

func TestLocalStoreBackendInitializeEmptyStoreDir(t *testing.T) {
	b := NewLocalStoreBackend("")
	err := b.Initialize(&Session{Initialized: true})
	if err == nil {
		t.Fatal("expected error for empty store dir")
	}
	code, _ := backendErrorDetails(err)
	if code != 501 {
		t.Fatalf("expected 501, got %d", code)
	}
}

func TestLocalStoreBackendDownloadNotFound(t *testing.T) {
	b := NewLocalStoreBackend(t.TempDir())
	session := &Session{Initialized: true}

	_, _, err := b.Download(session, validOID)
	if err == nil {
		t.Fatal("expected error for missing object")
	}
	code, _ := backendErrorDetails(err)
	if code != 404 {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestLocalStoreBackendUploadReportsProgress(t *testing.T) {
	var _ ProgressTransferBackend = (*LocalStoreBackend)(nil)

	b := NewLocalStoreBackend(t.TempDir())
	session := &Session{Initialized: true}
	payload := []byte(strings.Repeat("u", int(progressChunkSize*2+7)))
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])
	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to write upload payload: %v", err)
	}

	var calls []int64
	size, err := b.UploadWithProgress(session, oid, uploadPath, int64(len(payload)), func(bytesSoFar, bytesSinceLast int64) error {
		if bytesSinceLast <= 0 {
			t.Fatalf("bytesSinceLast must be positive, got %d", bytesSinceLast)
		}
		if len(calls) > 0 && bytesSoFar <= calls[len(calls)-1] {
			t.Fatalf("progress must increase, got previous=%d current=%d", calls[len(calls)-1], bytesSoFar)
		}
		calls = append(calls, bytesSoFar)
		return nil
	})
	if err != nil {
		t.Fatalf("UploadWithProgress returned error: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("uploaded size=%d, want %d", size, len(payload))
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 progress callbacks, got %d (%v)", len(calls), calls)
	}
	if calls[len(calls)-1] != int64(len(payload)) {
		t.Fatalf("final progress=%d, want %d", calls[len(calls)-1], len(payload))
	}
}

func TestLocalStoreBackendDownloadReportsProgress(t *testing.T) {
	b := NewLocalStoreBackend(t.TempDir())
	session := &Session{Initialized: true}
	payload := []byte(strings.Repeat("d", int(progressChunkSize+13)))
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	objectPath := b.objectPath(oid)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o700); err != nil {
		t.Fatalf("failed to prepare object directory: %v", err)
	}
	if err := os.WriteFile(objectPath, payload, 0o600); err != nil {
		t.Fatalf("failed to seed object: %v", err)
	}

	var calls []int64
	downloadPath, size, err := b.DownloadWithProgress(session, oid, func(bytesSoFar, bytesSinceLast int64) error {
		if bytesSinceLast <= 0 {
			t.Fatalf("bytesSinceLast must be positive, got %d", bytesSinceLast)
		}
		if len(calls) > 0 && bytesSoFar <= calls[len(calls)-1] {
			t.Fatalf("progress must increase, got previous=%d current=%d", calls[len(calls)-1], bytesSoFar)
		}
		calls = append(calls, bytesSoFar)
		return nil
	})
	if err != nil {
		t.Fatalf("DownloadWithProgress returned error: %v", err)
	}
	defer os.Remove(downloadPath)
	if size != int64(len(payload)) {
		t.Fatalf("download size=%d, want %d", size, len(payload))
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 progress callbacks, got %d (%v)", len(calls), calls)
	}
	if calls[len(calls)-1] != int64(len(payload)) {
		t.Fatalf("final progress=%d, want %d", calls[len(calls)-1], len(payload))
	}
	downloaded, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("failed to read staged download: %v", err)
	}
	if string(downloaded) != string(payload) {
		t.Fatal("downloaded payload mismatch")
	}
}

func TestClassifyErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		httpCode int
		want     ErrorCode
	}{
		{"401 -> auth_required", 401, ErrCodeAuthRequired},
		{"404 -> not_found", 404, ErrCodeNotFound},
		{"407 -> captcha_required", 407, ErrCodeCaptchaRequired},
		{"429 -> rate_limited", 429, ErrCodeRateLimited},
		{"503 -> server_error", 503, ErrCodeServerError},
		{"500 -> server_error", 500, ErrCodeServerError},
		{"502 -> server_error", 502, ErrCodeServerError},
		{"400 -> invalid_request", 400, ErrCodeInvalidRequest},
		{"403 -> invalid_request", 403, ErrCodeInvalidRequest},
		{"200 -> unknown", 200, ErrCodeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyErrorCode(tt.httpCode)
			if got != tt.want {
				t.Errorf("classifyErrorCode(%d) = %v, want %v", tt.httpCode, got, tt.want)
			}
		})
	}
}

func TestIsRetryableCode(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{429, false}, // Rate-limit is not retryable
		{404, false},
		{401, false},
		{200, false},
	}

	for _, tt := range tests {
		got := isRetryableCode(tt.code)
		if got != tt.want {
			t.Errorf("isRetryableCode(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestIsTemporaryCode(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{503, true},
		{500, true},
		{502, true},
		{504, true},
		{429, true},
		{404, false},
		{401, false},
		{200, false},
	}

	for _, tt := range tests {
		got := isTemporaryCode(tt.code)
		if got != tt.want {
			t.Errorf("isTemporaryCode(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestNewBackendErrorSetsStructuredFields(t *testing.T) {
	err := newBackendError(503, "service unavailable", errors.New("underlying error"))

	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatal("expected BackendError")
	}

	if backendErr.Code != 503 {
		t.Errorf("Code = %d, want 503", backendErr.Code)
	}
	if backendErr.ErrorCode != ErrCodeServerError {
		t.Errorf("ErrorCode = %v, want %v", backendErr.ErrorCode, ErrCodeServerError)
	}
	if !backendErr.Retryable {
		t.Error("expected Retryable = true for 503")
	}
	if !backendErr.Temporary {
		t.Error("expected Temporary = true for 503")
	}
}

func TestNewBackendErrorAuthRequired(t *testing.T) {
	err := newBackendError(401, "authentication required", nil)

	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatal("expected BackendError")
	}

	if backendErr.ErrorCode != ErrCodeAuthRequired {
		t.Errorf("ErrorCode = %v, want %v", backendErr.ErrorCode, ErrCodeAuthRequired)
	}
	if backendErr.Retryable {
		t.Error("expected Retryable = false for 401")
	}
	if backendErr.Temporary {
		t.Error("expected Temporary = false for 401")
	}
}

func TestNewBackendErrorRateLimited(t *testing.T) {
	err := newBackendError(429, "rate limited", nil)

	var backendErr *BackendError
	if !errors.As(err, &backendErr) {
		t.Fatal("expected BackendError")
	}

	if backendErr.ErrorCode != ErrCodeRateLimited {
		t.Errorf("ErrorCode = %v, want %v", backendErr.ErrorCode, ErrCodeRateLimited)
	}
	if backendErr.Retryable {
		t.Error("expected Retryable = false for 429")
	}
	if !backendErr.Temporary {
		t.Error("expected Temporary = true for 429")
	}
}
