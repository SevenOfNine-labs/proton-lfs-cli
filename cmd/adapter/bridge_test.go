package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the subprocess entry point used by Go's
// exec.Command testing pattern. Tests set GO_TEST_HELPER_PROCESS=1
// and pass "bridge <command>" as arguments.
func TestHelperProcess(_ *testing.T) {
	if os.Getenv("GO_TEST_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	// Find "bridge" in args to get the command
	bridgeIdx := -1
	for i, a := range args {
		if a == "bridge" {
			bridgeIdx = i
			break
		}
	}
	if bridgeIdx < 0 || bridgeIdx+1 >= len(args) {
		fmt.Fprintf(os.Stderr, "no bridge command found in args: %v", args)
		os.Exit(1)
	}
	command := args[bridgeIdx+1]

	// Read stdin JSON
	var req map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		writeErrorResponse(os.Stdout, 500, "failed to read stdin: "+err.Error())
		os.Exit(1)
	}

	if expectedJSON := os.Getenv("MOCK_BRIDGE_EXPECT_REQUEST"); expectedJSON != "" {
		var expected map[string]any
		if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
			writeErrorResponse(os.Stdout, 500, "failed to parse expected request: "+err.Error())
			os.Exit(1)
		}
		for key, want := range expected {
			if got, ok := req[key]; !ok || got != want {
				writeErrorResponse(os.Stdout, 500, fmt.Sprintf("request field %s = %v, want %v", key, got, want))
				os.Exit(1)
			}
		}
	}

	if forbiddenFields := os.Getenv("MOCK_BRIDGE_FORBID_FIELDS"); forbiddenFields != "" {
		for _, field := range strings.Split(forbiddenFields, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if _, ok := req[field]; ok {
				writeErrorResponse(os.Stdout, 500, fmt.Sprintf("forbidden request field present: %s", field))
				os.Exit(1)
			}
		}
	}

	// Check for mock error injection via env
	if mockErr := os.Getenv("MOCK_BRIDGE_ERROR"); mockErr != "" {
		code := 500
		if codeStr := os.Getenv("MOCK_BRIDGE_ERROR_CODE"); codeStr != "" {
			fmt.Sscanf(codeStr, "%d", &code)
		}
		writeErrorResponse(os.Stdout, code, mockErr)
		os.Exit(1)
	}

	// Check for mock delay
	if delayStr := os.Getenv("MOCK_BRIDGE_DELAY"); delayStr != "" {
		var d time.Duration
		d, _ = time.ParseDuration(delayStr)
		time.Sleep(d)
	}

	// Check for mock noise prefix (tests stdout noise tolerance)
	if noise := os.Getenv("MOCK_BRIDGE_NOISE"); noise != "" {
		fmt.Fprintln(os.Stdout, noise)
	}

	switch command {
	case "auth":
		writeOKResponse(os.Stdout, nil)
	case "init":
		writeOKResponse(os.Stdout, nil)
	case "upload":
		oid, _ := req["oid"].(string)
		if oid == "" {
			writeErrorResponse(os.Stdout, 400, "missing oid")
			os.Exit(1)
		}
		writeOKResponse(os.Stdout, nil)
	case "download":
		oid, _ := req["oid"].(string)
		outputPath, _ := req["outputPath"].(string)
		if oid == "" || outputPath == "" {
			writeErrorResponse(os.Stdout, 400, "missing oid or outputPath")
			os.Exit(1)
		}
		// Write test content to the output file
		content := os.Getenv("MOCK_BRIDGE_DOWNLOAD_CONTENT")
		if content == "" {
			content = "mock-download-content"
		}
		if err := os.WriteFile(outputPath, []byte(content), 0o600); err != nil {
			writeErrorResponse(os.Stdout, 500, "failed to write download: "+err.Error())
			os.Exit(1)
		}
		writeOKResponse(os.Stdout, nil)
	case "exists":
		existsResult := os.Getenv("MOCK_BRIDGE_EXISTS_RESULT")
		if existsResult == "false" {
			writeErrorResponse(os.Stdout, 404, "not found")
			os.Exit(1)
		}
		writeOKResponse(os.Stdout, map[string]bool{"exists": true})
	case "batch-exists":
		oids, _ := req["oids"].([]any)
		result := make(map[string]bool)
		for _, o := range oids {
			if s, ok := o.(string); ok {
				result[s] = true
			}
		}
		writeOKResponse(os.Stdout, result)
	case "batch-delete":
		oids, _ := req["oids"].([]any)
		result := make(map[string]bool)
		for _, o := range oids {
			if s, ok := o.(string); ok {
				result[s] = true
			}
		}
		writeOKResponse(os.Stdout, result)
	default:
		writeErrorResponse(os.Stdout, 400, "unknown command: "+command)
		os.Exit(1)
	}
}

func writeOKResponse(f *os.File, payload any) {
	resp := map[string]any{"ok": true}
	if payload != nil {
		payloadBytes, _ := json.Marshal(payload)
		resp["payload"] = json.RawMessage(payloadBytes)
	}
	json.NewEncoder(f).Encode(resp)
}

func writeErrorResponse(f *os.File, code int, message string) {
	resp := map[string]any{
		"ok":    false,
		"error": message,
		"code":  code,
	}
	if details := os.Getenv("MOCK_BRIDGE_ERROR_DETAILS"); details != "" {
		resp["details"] = details
	}
	json.NewEncoder(f).Encode(resp)
}

// helperBridgeClient creates a BridgeClient that uses the test binary as a mock subprocess.
func helperBridgeClient(t *testing.T, extraEnv ...string) *BridgeClient {
	t.Helper()
	env := []string{"GO_TEST_HELPER_PROCESS=1"}
	env = append(env, extraEnv...)
	return NewBridgeClient(BridgeClientConfig{
		NodeBin:       os.Args[0],
		CLIBin:        "-test.run=TestHelperProcess",
		Timeout:       10 * time.Second,
		MaxConcurrent: 10,
		StorageBase:   "LFS",
		AppVersion:    "test-1.0",
		ExtraEnv:      env,
	})
}

func TestBridgeAuthenticate(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	if err := bc.Authenticate(creds); err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
}

func TestBridgeInitLFSStorage(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	if err := bc.InitLFSStorage(creds); err != nil {
		t.Fatalf("InitLFSStorage failed: %v", err)
	}
}

func TestBridgeUpload(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	if err := bc.Upload(creds, validOID, "/tmp/test.bin"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
}

func TestBridgeDownload(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	tmpPath := t.TempDir() + "/download.bin"
	if err := bc.Download(creds, validOID, tmpPath); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(data) != "mock-download-content" {
		t.Fatalf("unexpected download content: %q", string(data))
	}
}

func TestBridgeExists(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	exists, err := bc.Exists(creds, validOID)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
}

func TestBridgeExistsNotFound(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_EXISTS_RESULT=false")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	exists, err := bc.Exists(creds, validOID)
	if err != nil {
		t.Fatalf("Exists should not error for 404: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for 404")
	}
}

func TestBridgeErrorMapping401(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_ERROR=unauthorized", "MOCK_BRIDGE_ERROR_CODE=401")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Authenticate(creds)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[401]") {
		t.Fatalf("expected [401] in error, got: %v", err)
	}
}

func TestBridgeErrorMapping404(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_ERROR=not found", "MOCK_BRIDGE_ERROR_CODE=404")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Upload(creds, validOID, "/tmp/test.bin")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[404]") {
		t.Fatalf("expected [404] in error, got: %v", err)
	}
}

func TestBridgeErrorMapping407(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_ERROR=captcha", "MOCK_BRIDGE_ERROR_CODE=407")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Authenticate(creds)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[407]") {
		t.Fatalf("expected [407] in error, got: %v", err)
	}
}

func TestBridgeErrorMapping429(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_ERROR=rate limited", "MOCK_BRIDGE_ERROR_CODE=429")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Authenticate(creds)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[429]") {
		t.Fatalf("expected [429] in error, got: %v", err)
	}
}

func TestBridgeErrorPreservesStructuredDetails(t *testing.T) {
	details := `{"errorCode":"TWO_FACTOR_REQUIRED","twoFactorType":"totp","totpAllowed":true}`
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_ERROR=Two-factor authentication code required",
		"MOCK_BRIDGE_ERROR_CODE=401",
		"MOCK_BRIDGE_ERROR_DETAILS="+details,
	)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Authenticate(creds)
	if err == nil {
		t.Fatal("expected error")
	}

	var bridgeErr *BridgeError
	if !strings.Contains(err.Error(), "[401]") {
		t.Fatalf("expected legacy [401] error string, got %v", err)
	}
	if !errors.As(err, &bridgeErr) {
		t.Fatalf("expected BridgeError, got %T", err)
	}
	if bridgeErr.Details != details {
		t.Fatalf("expected details %q, got %q", details, bridgeErr.Details)
	}
}

func TestBridgeStdoutNoiseTolerance(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_NOISE=DEBUG: some noisy log line")
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	if err := bc.Authenticate(creds); err != nil {
		t.Fatalf("Authenticate should succeed despite stdout noise: %v", err)
	}
}

func TestBridgeSemaphoreExhaustion(t *testing.T) {
	bc := NewBridgeClient(BridgeClientConfig{
		NodeBin:       os.Args[0],
		CLIBin:        "-test.run=TestHelperProcess",
		Timeout:       10 * time.Second,
		MaxConcurrent: 1,
		ExtraEnv:      []string{"GO_TEST_HELPER_PROCESS=1", "MOCK_BRIDGE_DELAY=2s"},
	})

	// Fill the semaphore
	bc.semaphore <- struct{}{}

	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	err := bc.Authenticate(creds)
	if err == nil {
		t.Fatal("expected concurrency limit error")
	}
	if !strings.Contains(err.Error(), "concurrency limit") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Release semaphore
	<-bc.semaphore
}

func TestBridgeCredentialPassthroughPassCLI(t *testing.T) {
	// This test verifies that credentials are included in the request
	// The mock subprocess doesn't validate them, but the bridge client
	// should include them in the JSON sent to stdin
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	if err := bc.Authenticate(creds); err != nil {
		t.Fatalf("Auth with pass-cli creds failed: %v", err)
	}
}

func TestBridgeCredentialPassthroughGitCredential(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderGitCredential}
	if err := bc.Authenticate(creds); err != nil {
		t.Fatalf("Auth with git-credential provider failed: %v", err)
	}
}

func TestBridgeDataCredentialSelectorPassthrough(t *testing.T) {
	expected := `{"credentialProvider":"git-credential","dataCredentialProvider":"git-credential","dataCredentialHost":"proton-data.proton-lfs-cli.local"}`
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_EXPECT_REQUEST="+expected,
		"MOCK_BRIDGE_FORBID_FIELDS=password,dataPassword",
	)
	creds := OperationCredentials{
		CredentialProvider:     CredentialProviderGitCredential,
		DataCredentialProvider: CredentialProviderGitCredential,
		DataCredentialHost:     DefaultDataCredentialHost,
	}
	if err := bc.Authenticate(creds); err != nil {
		t.Fatalf("Auth with data credential selectors failed: %v", err)
	}
}

func TestBridgeBatchExists(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	oids := []string{validOID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}
	result, err := bc.BatchExists(creds, oids)
	if err != nil {
		t.Fatalf("BatchExists failed: %v", err)
	}
	for _, oid := range oids {
		if !result[oid] {
			t.Fatalf("expected oid %s to exist", oid)
		}
	}
}

func TestBridgeBatchDelete(t *testing.T) {
	bc := helperBridgeClient(t)
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	oids := []string{validOID}
	result, err := bc.BatchDelete(creds, oids)
	if err != nil {
		t.Fatalf("BatchDelete failed: %v", err)
	}
	if !result[validOID] {
		t.Fatal("expected oid to be deleted")
	}
}

func TestParseBridgeOutput(t *testing.T) {
	t.Run("clean JSON", func(t *testing.T) {
		resp, err := parseBridgeOutput([]byte(`{"ok":true}`), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.OK {
			t.Fatal("expected OK=true")
		}
	})

	t.Run("JSON with noise", func(t *testing.T) {
		stdout := []byte("DEBUG: starting\nWARN: something\n{\"ok\":true}\n")
		resp, err := parseBridgeOutput(stdout, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.OK {
			t.Fatal("expected OK=true")
		}
	})

	t.Run("empty stdout", func(t *testing.T) {
		_, err := parseBridgeOutput([]byte(""), nil)
		if err == nil {
			t.Fatal("expected error for empty stdout")
		}
	})

	t.Run("no JSON", func(t *testing.T) {
		_, err := parseBridgeOutput([]byte("just some text\nno json here"), nil)
		if err == nil {
			t.Fatal("expected error for non-JSON output")
		}
	})
}

func TestParseBridgeOutputRejectsInvalidEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		wantErr string
	}{
		{
			name:    "missing ok",
			stdout:  `{"payload":{}}`,
			wantErr: "missing required ok",
		},
		{
			name:    "ok not boolean",
			stdout:  `{"ok":"true"}`,
			wantErr: "ok field must be boolean",
		},
		{
			name:    "ok null",
			stdout:  `{"ok":null}`,
			wantErr: "ok field must be boolean",
		},
		{
			name:    "success with error",
			stdout:  `{"ok":true,"error":"unexpected"}`,
			wantErr: "must not include an error message",
		},
		{
			name:    "success with code",
			stdout:  `{"ok":true,"code":200}`,
			wantErr: "must not include an error code",
		},
		{
			name:    "error without message",
			stdout:  `{"ok":false,"code":401}`,
			wantErr: "missing error message",
		},
		{
			name:    "error without code",
			stdout:  `{"ok":false,"error":"unauthorized"}`,
			wantErr: "missing positive error code",
		},
		{
			name:    "error with payload",
			stdout:  `{"ok":false,"error":"unauthorized","code":401,"payload":{}}`,
			wantErr: "must not include payload",
		},
		{
			name:    "unknown field",
			stdout:  `{"ok":true,"extra":1}`,
			wantErr: `unknown field "extra"`,
		},
		{
			name:    "code not integer",
			stdout:  `{"ok":false,"error":"unauthorized","code":"401"}`,
			wantErr: "code field must be integer",
		},
		{
			name:    "error not string",
			stdout:  `{"ok":false,"error":{"message":"unauthorized"},"code":401}`,
			wantErr: "error field must be string",
		},
		{
			name:    "code null",
			stdout:  `{"ok":false,"error":"unauthorized","code":null}`,
			wantErr: "code field must be integer",
		},
		{
			name:    "details null",
			stdout:  `{"ok":false,"error":"unauthorized","code":401,"details":null}`,
			wantErr: "details field must be string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBridgeOutput([]byte(tc.stdout), nil)
			if err == nil {
				t.Fatal("expected invalid envelope error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestBuildCredentials(t *testing.T) {
	t.Run("pass-cli provider", func(t *testing.T) {
		creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
		m := buildCredentials(creds, "LFS", "v1")
		if m["credentialProvider"] != CredentialProviderPassCLI {
			t.Fatalf("expected credentialProvider=pass-cli, got %v", m["credentialProvider"])
		}
		if m["storageBase"] != "LFS" {
			t.Fatalf("expected storageBase=LFS, got %v", m["storageBase"])
		}
		if m["appVersion"] != "v1" {
			t.Fatalf("expected appVersion=v1, got %v", m["appVersion"])
		}
	})

	t.Run("git-credential provider", func(t *testing.T) {
		creds := OperationCredentials{CredentialProvider: CredentialProviderGitCredential}
		m := buildCredentials(creds, "LFS", "")
		if m["credentialProvider"] != CredentialProviderGitCredential {
			t.Fatalf("expected credentialProvider=git-credential, got %v", m)
		}
		if _, ok := m["appVersion"]; ok {
			t.Fatal("appVersion should not be set when empty")
		}
	})

	t.Run("data credential provider", func(t *testing.T) {
		creds := OperationCredentials{
			CredentialProvider:     CredentialProviderGitCredential,
			DataCredentialProvider: CredentialProviderGitCredential,
			DataCredentialHost:     DefaultDataCredentialHost,
		}
		m := buildCredentials(creds, "LFS", "")
		if m["credentialProvider"] != CredentialProviderGitCredential {
			t.Fatalf("expected login credential provider, got %v", m)
		}
		if m["dataCredentialProvider"] != CredentialProviderGitCredential {
			t.Fatalf("expected data credential provider, got %v", m)
		}
		if m["dataCredentialHost"] != DefaultDataCredentialHost {
			t.Fatalf("expected data credential host, got %v", m)
		}
		if _, ok := m["dataPassword"]; ok {
			t.Fatal("dataPassword must never be placed in bridge credential selectors")
		}
	})

	t.Run("empty provider", func(t *testing.T) {
		creds := OperationCredentials{}
		m := buildCredentials(creds, "LFS", "")
		if _, ok := m["credentialProvider"]; ok {
			t.Fatal("credentialProvider should not be set when empty")
		}
		if m["storageBase"] != "LFS" {
			t.Fatalf("expected storageBase=LFS, got %v", m["storageBase"])
		}
	})
}

func TestFilteredEnvAllowlist(t *testing.T) {
	bc := &BridgeClient{extraEnv: []string{"EXTRA_VAR=1"}}

	env := bc.filteredEnv()
	// Should contain at least PATH and HOME from the real environment
	var hasPath, hasExtra bool
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
		}
		if e == "EXTRA_VAR=1" {
			hasExtra = true
		}
	}
	if !hasPath {
		t.Fatal("expected PATH in filtered env")
	}
	if !hasExtra {
		t.Fatal("expected EXTRA_VAR in filtered env")
	}
}

func TestMatchesAllowlist(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"PATH", true},
		{"HOME", true},
		{"NODE_ENV", true},
		{"MOCK_BRIDGE_FOO", true},
		{"PROTON_LFS_BACKEND", true},
		{"SECRET_KEY", false},
		{"AWS_ACCESS_KEY_ID", false},
	}
	for _, tc := range cases {
		if got := matchesAllowlist(tc.key); got != tc.want {
			t.Errorf("matchesAllowlist(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// --- Additional Bridge Tests ---

func TestSanitizeStderr(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{"empty", "", func(s string) bool { return s == "" }},
		{"normal text", "some error occurred", func(s string) bool { return s == "some error occurred" }},
		{"Bearer redaction", "auth failed: Bearer eyJhbGciOiJSUz...", func(s string) bool {
			return strings.HasSuffix(s, "[redacted]") && !strings.Contains(s, "eyJ")
		}},
		{"session redaction", "debug: session=abc123 extra", func(s string) bool {
			return strings.HasSuffix(s, "[redacted]") && !strings.Contains(s, "abc123")
		}},
		{"token redaction", "error: token=xyz456", func(s string) bool {
			return strings.HasSuffix(s, "[redacted]") && !strings.Contains(s, "xyz456")
		}},
		{"256 char cap", strings.Repeat("x", 300), func(s string) bool {
			return len(s) <= 260 && strings.HasSuffix(s, "...")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeStderr(tc.input)
			if !tc.check(result) {
				t.Fatalf("sanitizeStderr(%q) = %q, check failed", tc.input, result)
			}
		})
	}
}

func TestResolveNodeBinaryFromEnv(t *testing.T) {
	t.Setenv("NODE_BIN", "/custom/node")
	got := resolveNodeBinary()
	if got != "/custom/node" {
		t.Fatalf("expected /custom/node, got %q", got)
	}
}

func TestResolveNodeBinaryFallback(t *testing.T) {
	t.Setenv("NODE_BIN", "")
	got := resolveNodeBinary()
	if got == "" {
		t.Fatal("expected non-empty node binary path")
	}
}

func TestParseBridgeOutputWithPayload(t *testing.T) {
	stdout := []byte(`{"ok":true,"payload":{"exists":true}}`)
	resp, err := parseBridgeOutput(stdout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
	if len(resp.Payload) == 0 {
		t.Fatal("expected non-empty payload")
	}
}

func TestParseBridgeOutputErrorResponse(t *testing.T) {
	stdout := []byte(`{"ok":false,"error":"not found","code":404}`)
	resp, err := parseBridgeOutput(stdout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected OK=false")
	}
	if resp.Error != "not found" {
		t.Fatalf("expected error 'not found', got %q", resp.Error)
	}
	if resp.Code != 404 {
		t.Fatalf("expected code 404, got %d", resp.Code)
	}
}

func TestParseBridgeOutputMultipleLines(t *testing.T) {
	stdout := []byte("{\"ok\":false,\"error\":\"first\"}\n{\"ok\":true}\n")
	resp, err := parseBridgeOutput(stdout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected last JSON line (OK=true) to win")
	}
}

func TestBuildCredentialsEmptyStorageBase(t *testing.T) {
	creds := OperationCredentials{CredentialProvider: CredentialProviderPassCLI}
	m := buildCredentials(creds, "", "v1")
	if _, ok := m["storageBase"]; ok {
		t.Fatal("storageBase should be absent when empty")
	}
}

func TestBuildCredentialsEmptyProvider(t *testing.T) {
	creds := OperationCredentials{CredentialProvider: ""}
	m := buildCredentials(creds, "LFS", "v1")
	if _, ok := m["credentialProvider"]; ok {
		t.Fatal("credentialProvider should be absent when empty")
	}
	if m["storageBase"] != "LFS" {
		t.Fatal("storageBase should still be present")
	}
}
