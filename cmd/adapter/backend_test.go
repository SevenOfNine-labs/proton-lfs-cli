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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	// Initialize (auth + init)
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
	backend := NewDriveCLIBackend(bc, "")
	session := &Session{Initialized: true, CreatedAt: time.Now()}

	// With empty provider, auth is delegated to proton-drive-cli
	// which will attempt resolution and succeed (mock bridge returns ok)
	err := backend.Initialize(session)
	if err != nil {
		t.Fatalf("Initialize with empty provider should succeed (delegated): %v", err)
	}
}

func TestDriveCLIBackendUploadMapsNotFoundError(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_EXISTS_RESULT=false",
		"MOCK_BRIDGE_ERROR=not found",
		"MOCK_BRIDGE_ERROR_CODE=404",
	)
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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

func TestDriveCLIBackendGitCredentialMode(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge:             bc,
		credentialProvider: CredentialProviderGitCredential,
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

func TestMapBridgeErrorNil(t *testing.T) {
	if err := mapBridgeError(nil, "fallback"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestDriveCLIBackendUploadNotAuthenticated(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
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
	backend := NewDriveCLIBackend(bc, CredentialProviderPassCLI)
	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	code, _ := backendErrorDetails(err)
	if code != 429 {
		t.Fatalf("expected 429, got %d (%v)", code, err)
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
		{429, false},
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
}
