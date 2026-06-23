package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	bridgeIdx := -1
	for i, arg := range os.Args {
		if arg == "bridge" {
			bridgeIdx = i
			break
		}
	}
	if bridgeIdx < 0 || bridgeIdx+1 >= len(os.Args) {
		fmt.Fprintf(os.Stderr, "no bridge command found in args: %v", os.Args)
		os.Exit(1)
	}
	command := os.Args[bridgeIdx+1]

	var req map[string]any
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		writeErrorResponse(os.Stdout, 500, "failed to read stdin: "+err.Error())
		os.Exit(1)
	}

	if logPath := os.Getenv("MOCK_BRIDGE_COMMAND_LOG"); logPath != "" {
		requestBytes, err := json.Marshal(req)
		if err != nil {
			writeErrorResponse(os.Stdout, 500, "failed to marshal command log request: "+err.Error())
			os.Exit(1)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			writeErrorResponse(os.Stdout, 500, "failed to open command log: "+err.Error())
			os.Exit(1)
		}
		if _, err := f.WriteString(command + "\t" + string(requestBytes) + "\n"); err != nil {
			_ = f.Close()
			writeErrorResponse(os.Stdout, 500, "failed to write command log: "+err.Error())
			os.Exit(1)
		}
		if err := f.Close(); err != nil {
			writeErrorResponse(os.Stdout, 500, "failed to close command log: "+err.Error())
			os.Exit(1)
		}
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

	if rawStdout, ok := os.LookupEnv("MOCK_BRIDGE_RAW_STDOUT"); ok {
		fmt.Fprint(os.Stdout, rawStdout)
		if rawStderr := os.Getenv("MOCK_BRIDGE_RAW_STDERR"); rawStderr != "" {
			fmt.Fprint(os.Stderr, rawStderr)
		}
		code := 0
		if codeStr := os.Getenv("MOCK_BRIDGE_RAW_EXIT_CODE"); codeStr != "" {
			fmt.Sscanf(codeStr, "%d", &code)
		}
		os.Exit(code)
	}

	if mockErr := os.Getenv("MOCK_BRIDGE_ERROR"); mockErr != "" {
		errorCommand := strings.TrimSpace(os.Getenv("MOCK_BRIDGE_ERROR_COMMAND"))
		if errorCommand == "" || errorCommand == command {
			code := 500
			if codeStr := os.Getenv("MOCK_BRIDGE_ERROR_CODE"); codeStr != "" {
				fmt.Sscanf(codeStr, "%d", &code)
			}
			writeErrorResponse(os.Stdout, code, mockErr)
			os.Exit(1)
		}
	}

	if delayStr := os.Getenv("MOCK_BRIDGE_DELAY"); delayStr != "" {
		delay, _ := time.ParseDuration(delayStr)
		time.Sleep(delay)
	}

	if noise := os.Getenv("MOCK_BRIDGE_NOISE"); noise != "" {
		fmt.Fprintln(os.Stdout, noise)
	}

	switch command {
	case "auth-state":
		state := os.Getenv("MOCK_BRIDGE_AUTH_STATE")
		if state == "" {
			state = "ready"
		}
		hasSession := state != "needs_login"
		sessionValid := state == "ready" || state == "needs_data_password" || state == "needs_key_password"
		passwordMode := 1
		if state == "needs_data_password" {
			passwordMode = 2
		}
		authMode := os.Getenv("MOCK_BRIDGE_AUTH_MODE")
		if authMode == "" && (state == "ready" || state == "needs_key_password") {
			authMode = "browser-fork"
		}
		keyPasswordPersisted := os.Getenv("MOCK_BRIDGE_KEY_PASSWORD_PERSISTED") == "true" || state == "needs_key_password"
		keyPasswordAvailable := os.Getenv("MOCK_BRIDGE_KEY_PASSWORD_AVAILABLE") == "true" || (keyPasswordPersisted && state != "needs_key_password")
		keyPasswordProvider := os.Getenv("MOCK_BRIDGE_KEY_PASSWORD_PROVIDER")
		if keyPasswordProvider == "" && keyPasswordPersisted {
			keyPasswordProvider = "git-credential"
		}
		keyPasswordHost := os.Getenv("MOCK_BRIDGE_KEY_PASSWORD_HOST")
		if keyPasswordHost == "" && keyPasswordPersisted {
			keyPasswordHost = "proton-drive-key.proton-lfs-cli.local"
		}
		writeOKResponse(os.Stdout, map[string]any{
			"state":                   state,
			"hasSession":              hasSession,
			"sessionValid":            sessionValid,
			"sessionExpired":          state == "session_expired",
			"sessionUidPresent":       hasSession,
			"passwordMode":            passwordMode,
			"authMode":                authMode,
			"keyPasswordPersisted":    keyPasswordPersisted,
			"keyPasswordAvailable":    keyPasswordAvailable,
			"keyPasswordProvider":     keyPasswordProvider,
			"keyPasswordHost":         keyPasswordHost,
			"hasExplicitDataPassword": false,
			"dataCredentialProvider":  req["dataCredentialProvider"],
			"dataCredentialHost":      req["dataCredentialHost"],
			"willAttemptNetwork":      false,
			"errors":                  []string{},
			"actions":                 []string{},
		})
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
		if os.Getenv("MOCK_BRIDGE_EXISTS_RESULT") == "false" {
			writeErrorResponse(os.Stdout, 404, "not found")
			os.Exit(1)
		}
		writeOKResponse(os.Stdout, map[string]any{"oid": req["oid"], "exists": true})
	case "batch-exists":
		oids, _ := req["oids"].([]any)
		result := make(map[string]bool)
		for _, oid := range oids {
			if value, ok := oid.(string); ok {
				result[value] = true
			}
		}
		writeOKResponse(os.Stdout, map[string]any{"results": result})
	case "batch-delete":
		oids, _ := req["oids"].([]any)
		result := make(map[string]bool)
		for _, oid := range oids {
			if value, ok := oid.(string); ok {
				result[value] = true
			}
		}
		writeOKResponse(os.Stdout, map[string]any{"results": result})
	default:
		writeErrorResponse(os.Stdout, 400, "unsupported command: "+command)
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

type loggedBridgeCommand struct {
	Command string
	Request map[string]any
}

func readLoggedBridgeCommands(t *testing.T, logPath string) []loggedBridgeCommand {
	t.Helper()
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read bridge command log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	commands := make([]loggedBridgeCommand, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid bridge command log line: %q", line)
		}
		var request map[string]any
		if err := json.Unmarshal([]byte(parts[1]), &request); err != nil {
			t.Fatalf("failed to parse bridge command request %q: %v", parts[1], err)
		}
		commands = append(commands, loggedBridgeCommand{Command: parts[0], Request: request})
	}
	return commands
}

func assertBridgeCommands(t *testing.T, commands []loggedBridgeCommand, want ...string) {
	t.Helper()
	got := make([]string, 0, len(commands))
	for _, command := range commands {
		got = append(got, command.Command)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bridge commands = %v, want %v", got, want)
	}
}

func TestBridgeInitLFSStorage(t *testing.T) {
	bc := helperBridgeClient(t)
	if err := bc.InitLFSStorage(OperationCredentials{}); err != nil {
		t.Fatalf("InitLFSStorage failed: %v", err)
	}
}

func TestBridgeUpload(t *testing.T) {
	bc := helperBridgeClient(t)
	if err := bc.Upload(OperationCredentials{}, validOID, "/tmp/test.bin"); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
}

func TestBridgeDownload(t *testing.T) {
	bc := helperBridgeClient(t)
	tmpPath := filepath.Join(t.TempDir(), "download.bin")
	if err := bc.Download(OperationCredentials{}, validOID, tmpPath); err != nil {
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
	exists, err := bc.Exists(OperationCredentials{}, validOID)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
}

func TestBridgeExistsNotFound(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_EXISTS_RESULT=false")
	exists, err := bc.Exists(OperationCredentials{}, validOID)
	if err != nil {
		t.Fatalf("Exists should not error for 404: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for 404")
	}
}

func TestBridgeExistsRejectsMalformedSuccessPayload(t *testing.T) {
	bc := helperBridgeClient(t, `MOCK_BRIDGE_RAW_STDOUT={"ok":true,"payload":{"exists":"true"}}`)
	exists, err := bc.Exists(OperationCredentials{}, validOID)
	if err == nil {
		t.Fatalf("expected malformed exists payload to fail, got exists=%v", exists)
	}
	if !strings.Contains(err.Error(), "exists field must be boolean") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridgeErrorMapping404(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_ERROR=not found", "MOCK_BRIDGE_ERROR_CODE=404")
	err := bc.Upload(OperationCredentials{}, validOID, "/tmp/test.bin")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "[404]") {
		t.Fatalf("expected [404] in error, got: %v", err)
	}
}

func TestBridgeAuthState(t *testing.T) {
	bc := helperBridgeClient(t)
	state, err := bc.AuthState(OperationCredentials{})
	if err != nil {
		t.Fatalf("AuthState failed: %v", err)
	}
	if state.State != "ready" {
		t.Fatalf("expected ready auth state, got %q", state.State)
	}
	if state.WillAttemptNetwork {
		t.Fatal("auth-state must remain offline-only")
	}
}

func TestBridgeAuthStateKeyPasswordDiagnostics(t *testing.T) {
	bc := helperBridgeClient(t, "MOCK_BRIDGE_AUTH_STATE=needs_key_password")
	state, err := bc.AuthState(OperationCredentials{})
	if err != nil {
		t.Fatalf("AuthState failed: %v", err)
	}
	if state.State != "needs_key_password" {
		t.Fatalf("expected needs_key_password auth state, got %q", state.State)
	}
	if state.AuthMode != "browser-fork" {
		t.Fatalf("expected browser-fork auth mode, got %q", state.AuthMode)
	}
	if !state.KeyPasswordPersisted {
		t.Fatal("expected persisted key-password metadata")
	}
	if state.KeyPasswordAvailable {
		t.Fatal("expected key password to be unavailable")
	}
	if state.KeyPasswordProvider != "git-credential" {
		t.Fatalf("expected git-credential key-password provider, got %q", state.KeyPasswordProvider)
	}
	if state.KeyPasswordHost != "proton-drive-key.proton-lfs-cli.local" {
		t.Fatalf("unexpected key-password host %q", state.KeyPasswordHost)
	}
	if state.WillAttemptNetwork {
		t.Fatal("auth-state must remain offline-only")
	}
}

func TestBridgeErrorPreservesStructuredDetails(t *testing.T) {
	details := `{"errorCode":"KEY_PASSWORD_REQUIRED"}`
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_ERROR=Browser-fork session is missing its stored key password",
		"MOCK_BRIDGE_ERROR_CODE=401",
		"MOCK_BRIDGE_ERROR_DETAILS="+details,
	)
	err := bc.InitLFSStorage(OperationCredentials{})
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
	if err := bc.InitLFSStorage(OperationCredentials{}); err != nil {
		t.Fatalf("InitLFSStorage should succeed despite stdout noise: %v", err)
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
	bc.semaphore <- struct{}{}

	err := bc.InitLFSStorage(OperationCredentials{})
	if err == nil {
		t.Fatal("expected concurrency limit error")
	}
	if !strings.Contains(err.Error(), "concurrency limit") {
		t.Fatalf("unexpected error: %v", err)
	}

	<-bc.semaphore
}

func TestBridgeCommandTimeoutReturnsTypedError(t *testing.T) {
	bc := NewBridgeClient(BridgeClientConfig{
		NodeBin:       os.Args[0],
		CLIBin:        "-test.run=TestHelperProcess",
		Timeout:       20 * time.Millisecond,
		MaxConcurrent: 1,
		ExtraEnv:      []string{"GO_TEST_HELPER_PROCESS=1", "MOCK_BRIDGE_DELAY=250ms"},
	})

	start := time.Now()
	err := bc.InitLFSStorage(OperationCredentials{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout test took too long: %s", time.Since(start))
	}
	if !strings.Contains(err.Error(), "timed out after") {
		t.Fatalf("expected typed timeout error, got %v", err)
	}
}

func TestBridgeCommandRejectsPartialJSONOutput(t *testing.T) {
	bc := helperBridgeClient(t, `MOCK_BRIDGE_RAW_STDOUT={"ok":true`)
	err := bc.InitLFSStorage(OperationCredentials{})
	if err == nil {
		t.Fatal("expected partial JSON output error")
	}
	if !strings.Contains(err.Error(), "no valid JSON envelope found") {
		t.Fatalf("expected malformed output error, got %v", err)
	}
}

func TestBridgeCommandSanitizesStderrOnMalformedOutput(t *testing.T) {
	bc := helperBridgeClient(t,
		"MOCK_BRIDGE_RAW_STDOUT=not-json",
		"MOCK_BRIDGE_RAW_STDERR=fatal auth failed: Bearer secret-token",
		"MOCK_BRIDGE_RAW_EXIT_CODE=1",
	)
	err := bc.InitLFSStorage(OperationCredentials{})
	if err == nil {
		t.Fatal("expected subprocess failure")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("stderr leaked secret: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected sanitized stderr, got %v", err)
	}
}

func TestBridgeCommandsNeverSendAccountLoginFields(t *testing.T) {
	forbidden := []string{
		"username",
		"password",
		"credentialProvider",
		"allowLogin",
		"secondFactorCode",
	}
	creds := OperationCredentials{
		DataCredentialProvider: CredentialProviderGitCredential,
		DataCredentialHost:     DefaultDataCredentialHost,
	}
	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("upload"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	cases := []struct {
		name        string
		wantCommand string
		run         func(*BridgeClient) error
	}{
		{name: "auth-state", wantCommand: "auth-state", run: func(bc *BridgeClient) error {
			_, err := bc.AuthState(creds)
			return err
		}},
		{name: "init", wantCommand: "init", run: func(bc *BridgeClient) error {
			return bc.InitLFSStorage(creds)
		}},
		{name: "upload", wantCommand: "upload", run: func(bc *BridgeClient) error {
			return bc.Upload(creds, validOID, uploadPath)
		}},
		{name: "download", wantCommand: "download", run: func(bc *BridgeClient) error {
			return bc.Download(creds, validOID, filepath.Join(t.TempDir(), "download.bin"))
		}},
		{name: "exists", wantCommand: "exists", run: func(bc *BridgeClient) error {
			_, err := bc.Exists(creds, validOID)
			return err
		}},
		{name: "batch-exists", wantCommand: "batch-exists", run: func(bc *BridgeClient) error {
			_, err := bc.batchExists(creds, []string{validOID})
			return err
		}},
		{name: "batch-delete", wantCommand: "batch-delete", run: func(bc *BridgeClient) error {
			_, err := bc.batchDelete(creds, []string{validOID})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "bridge-commands.log")
			bc := helperBridgeClient(t, "MOCK_BRIDGE_COMMAND_LOG="+logPath)
			if err := tc.run(bc); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}

			commands := readLoggedBridgeCommands(t, logPath)
			assertBridgeCommands(t, commands, tc.wantCommand)
			request := commands[0].Request
			for _, field := range forbidden {
				if _, ok := request[field]; ok {
					t.Fatalf("%s request must not include %s: %#v", tc.name, field, request)
				}
			}
			if request["dataCredentialProvider"] != CredentialProviderGitCredential {
				t.Fatalf("%s request should preserve dataCredentialProvider, got %#v", tc.name, request)
			}
			if request["dataCredentialHost"] != DefaultDataCredentialHost {
				t.Fatalf("%s request should preserve dataCredentialHost, got %#v", tc.name, request)
			}
		})
	}
}

func TestBridgeBatchExistsMaintenanceHelper(t *testing.T) {
	bc := helperBridgeClient(t)
	oids := []string{validOID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}
	result, err := bc.batchExists(OperationCredentials{}, oids)
	if err != nil {
		t.Fatalf("batchExists failed: %v", err)
	}
	for _, oid := range oids {
		if !result[oid] {
			t.Fatalf("expected oid %s to exist", oid)
		}
	}
}

func TestBridgeBatchDeleteMaintenanceHelper(t *testing.T) {
	bc := helperBridgeClient(t)
	result, err := bc.batchDelete(OperationCredentials{}, []string{validOID})
	if err != nil {
		t.Fatalf("batchDelete failed: %v", err)
	}
	if !result[validOID] {
		t.Fatal("expected oid to be deleted")
	}
}

func TestParseBridgeBoolMapPayload(t *testing.T) {
	rawMap, err := parseBridgeBoolMapPayload("batch-exists", json.RawMessage(`{"`+validOID+`":true}`))
	if err != nil {
		t.Fatalf("raw map parse failed: %v", err)
	}
	if !rawMap[validOID] {
		t.Fatal("expected raw map oid result")
	}

	wrapped, err := parseBridgeBoolMapPayload("batch-delete", json.RawMessage(`{"results":{"`+validOID+`":true}}`))
	if err != nil {
		t.Fatalf("wrapped results parse failed: %v", err)
	}
	if !wrapped[validOID] {
		t.Fatal("expected wrapped oid result")
	}

	if _, err := parseBridgeBoolMapPayload("batch-delete", nil); err == nil {
		t.Fatal("expected empty batch payload to fail")
	}

	if _, err := parseBridgeBoolMapPayload("batch-delete", json.RawMessage(`{"not-an-oid":true}`)); err == nil {
		t.Fatal("expected invalid oid key to fail")
	}
}

func TestParseBridgeExistsPayload(t *testing.T) {
	got, err := parseBridgeExistsPayload(json.RawMessage(`{"oid":"` + validOID + `","exists":true}`))
	if err != nil {
		t.Fatalf("valid exists payload failed: %v", err)
	}
	if !got {
		t.Fatal("expected exists=true")
	}

	cases := []struct {
		name    string
		payload json.RawMessage
		wantErr string
	}{
		{name: "empty", payload: nil, wantErr: "missing payload"},
		{name: "missing exists", payload: json.RawMessage(`{"oid":"` + validOID + `"}`), wantErr: "missing exists"},
		{name: "unknown field", payload: json.RawMessage(`{"exists":true,"extra":1}`), wantErr: "unknown field"},
		{name: "exists string", payload: json.RawMessage(`{"exists":"true"}`), wantErr: "must be boolean"},
		{name: "bad oid", payload: json.RawMessage(`{"oid":"bad","exists":true}`), wantErr: "64-character hex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBridgeExistsPayload(tc.payload)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
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
		{name: "missing ok", stdout: `{"payload":{}}`, wantErr: "missing required ok"},
		{name: "ok not boolean", stdout: `{"ok":"true"}`, wantErr: "ok field must be boolean"},
		{name: "ok null", stdout: `{"ok":null}`, wantErr: "ok field must be boolean"},
		{name: "success with error", stdout: `{"ok":true,"error":"unexpected"}`, wantErr: "must not include an error message"},
		{name: "success with code", stdout: `{"ok":true,"code":200}`, wantErr: "must not include an error code"},
		{name: "error without message", stdout: `{"ok":false,"code":401}`, wantErr: "missing error message"},
		{name: "error without code", stdout: `{"ok":false,"error":"unauthorized"}`, wantErr: "missing positive error code"},
		{name: "error with payload", stdout: `{"ok":false,"error":"unauthorized","code":401,"payload":{}}`, wantErr: "must not include payload"},
		{name: "unknown field", stdout: `{"ok":true,"extra":1}`, wantErr: `unknown field "extra"`},
		{name: "code not integer", stdout: `{"ok":false,"error":"unauthorized","code":"401"}`, wantErr: "code field must be integer"},
		{name: "error not string", stdout: `{"ok":false,"error":{"message":"unauthorized"},"code":401}`, wantErr: "error field must be string"},
		{name: "code null", stdout: `{"ok":false,"error":"unauthorized","code":null}`, wantErr: "code field must be integer"},
		{name: "details null", stdout: `{"ok":false,"error":"unauthorized","code":401,"details":null}`, wantErr: "details field must be string"},
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
	t.Run("ignores account credential provider", func(t *testing.T) {
		m := buildCredentials(OperationCredentials{}, "LFS", "v1")
		if _, ok := m["credentialProvider"]; ok {
			t.Fatalf("credentialProvider must not be sent to bridge: %#v", m)
		}
		if _, ok := m["allowLogin"]; ok {
			t.Fatalf("allowLogin must not be sent to bridge: %#v", m)
		}
		if m["storageBase"] != "LFS" {
			t.Fatalf("expected storageBase=LFS, got %v", m["storageBase"])
		}
		if m["appVersion"] != "v1" {
			t.Fatalf("expected appVersion=v1, got %v", m["appVersion"])
		}
	})

	t.Run("data credential provider", func(t *testing.T) {
		creds := OperationCredentials{
			DataCredentialProvider: CredentialProviderGitCredential,
			DataCredentialHost:     DefaultDataCredentialHost,
		}
		m := buildCredentials(creds, "LFS", "")
		if _, ok := m["credentialProvider"]; ok {
			t.Fatalf("credentialProvider must not be sent to bridge: %#v", m)
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

	t.Run("empty storage base", func(t *testing.T) {
		m := buildCredentials(OperationCredentials{}, "", "v1")
		if _, ok := m["storageBase"]; ok {
			t.Fatal("storageBase should be absent when empty")
		}
	})
}

func TestFilteredEnvAllowlist(t *testing.T) {
	bc := &BridgeClient{extraEnv: []string{"EXTRA_VAR=1"}}

	env := bc.filteredEnv()
	var hasPath, hasExtra bool
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			hasPath = true
		}
		if item == "EXTRA_VAR=1" {
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
	stdout := []byte("{\"ok\":false,\"error\":\"first\",\"code\":500}\n{\"ok\":true}\n")
	resp, err := parseBridgeOutput(stdout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected last JSON line (OK=true) to win")
	}
}
