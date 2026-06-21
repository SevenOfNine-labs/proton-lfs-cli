package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// BridgeResponse is the JSON envelope returned by proton-drive-cli bridge commands.
type BridgeResponse struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
	Code    int             `json:"code,omitempty"`
	Details string          `json:"details,omitempty"`
}

// BridgeError preserves the structured error envelope returned by
// proton-drive-cli while keeping the legacy "[code] message" string shape.
type BridgeError struct {
	Command string
	Code    int
	Message string
	Details string
}

func (e *BridgeError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "unknown bridge error"
	}
	if e.Code > 0 {
		return fmt.Sprintf("[%d] %s", e.Code, msg)
	}
	return msg
}

// BridgeClientConfig holds the configuration for creating a new BridgeClient.
type BridgeClientConfig struct {
	NodeBin       string
	CLIBin        string
	Timeout       time.Duration
	MaxConcurrent int
	StorageBase   string
	AppVersion    string
	ExtraEnv      []string // additional env vars (for testing)
}

// BridgeAuthStateResponse mirrors proton-drive-cli bridge auth-state payloads.
// It is intentionally local-only on the drive-cli side: no credential lookup,
// no token refresh, and no Proton network request.
type BridgeAuthStateResponse struct {
	State                   string   `json:"state"`
	HasSession              bool     `json:"hasSession"`
	SessionValid            bool     `json:"sessionValid"`
	SessionExpired          bool     `json:"sessionExpired"`
	SessionUIDPresent       bool     `json:"sessionUidPresent"`
	PasswordMode            int      `json:"passwordMode,omitempty"`
	UsernamePresent         bool     `json:"usernamePresent"`
	HasExplicitLoginPass    bool     `json:"hasExplicitLoginPassword"`
	HasExplicitDataPass     bool     `json:"hasExplicitDataPassword"`
	LoginCredentialProvider string   `json:"loginCredentialProvider,omitempty"`
	DataCredentialProvider  string   `json:"dataCredentialProvider,omitempty"`
	DataCredentialHost      string   `json:"dataCredentialHost,omitempty"`
	AllowLogin              bool     `json:"allowLogin"`
	WillAttemptNetwork      bool     `json:"willAttemptNetwork"`
	Errors                  []string `json:"errors"`
	Actions                 []string `json:"actions"`
}

// BridgeClient communicates with proton-drive-cli via subprocess stdin/stdout.
type BridgeClient struct {
	nodeBin       string
	cliBin        string
	timeout       time.Duration
	maxConcurrent int
	semaphore     chan struct{}
	storageBase   string
	appVersion    string
	extraEnv      []string
}

// NewBridgeClient creates a new bridge subprocess client.
func NewBridgeClient(cfg BridgeClientConfig) *BridgeClient {
	if cfg.NodeBin == "" {
		cfg.NodeBin = resolveNodeBinary()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	if cfg.StorageBase == "" {
		cfg.StorageBase = DefaultStorageBase
	}
	return &BridgeClient{
		nodeBin:       cfg.NodeBin,
		cliBin:        cfg.CLIBin,
		timeout:       cfg.Timeout,
		maxConcurrent: cfg.MaxConcurrent,
		semaphore:     make(chan struct{}, cfg.MaxConcurrent),
		storageBase:   cfg.StorageBase,
		appVersion:    cfg.AppVersion,
		extraEnv:      cfg.ExtraEnv,
	}
}

// envAllowlist lists environment variable prefixes and exact names that are
// forwarded to the bridge subprocess. This mirrors the allowlist previously
// maintained in protonDriveBridge.js.
var envAllowlist = []string{
	"PATH",
	"HOME",
	"USER",
	"SHELL",
	"LANG",
	"LC_",
	"TERM",
	"NODE_ENV",
	"NODE_OPTIONS",
	"NODE_PATH",
	"NODE_BIN",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_CACHE_HOME",
	"XDG_RUNTIME_DIR",
	"MOCK_BRIDGE_",
	"PROTON_",
	"LFS_",
	"SDK_",
	"TMPDIR",
	"TMP",
	"TEMP",
}

// filteredEnv returns environment variables that match the allowlist.
func (bc *BridgeClient) filteredEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		if matchesAllowlist(key) {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, bc.extraEnv...)
	return filtered
}

func matchesAllowlist(key string) bool {
	for _, allowed := range envAllowlist {
		if strings.HasSuffix(allowed, "_") {
			// Prefix match
			if strings.HasPrefix(key, allowed) {
				return true
			}
		} else if key == allowed {
			return true
		}
	}
	return false
}

// runBridgeCommand executes a proton-drive-cli bridge command as a subprocess.
func (bc *BridgeClient) runBridgeCommand(command string, request map[string]any) (*BridgeResponse, error) {
	// Non-blocking semaphore acquire
	select {
	case bc.semaphore <- struct{}{}:
		defer func() { <-bc.semaphore }()
	default:
		return nil, fmt.Errorf("bridge concurrency limit reached (%d)", bc.maxConcurrent)
	}

	ctx, cancel := context.WithTimeout(context.Background(), bc.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bc.nodeBin, bc.cliBin, "bridge", command)
	cmd.Env = bc.filteredEnv()

	stdinBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bridge request: %w", err)
	}
	cmd.Stdin = bytes.NewReader(stdinBytes)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	resp, parseErr := parseBridgeOutput(stdout.Bytes(), stderr.Bytes())
	if parseErr != nil {
		if err != nil {
			stderrText := sanitizeStderr(stderr.String())
			if stderrText != "" {
				return nil, fmt.Errorf("bridge %s failed: %s", command, stderrText)
			}
			return nil, fmt.Errorf("bridge %s failed: %w", command, err)
		}
		return nil, fmt.Errorf("bridge %s: %w", command, parseErr)
	}

	if !resp.OK {
		errMsg := resp.Error
		if errMsg == "" {
			errMsg = "unknown bridge error"
		}

		return resp, &BridgeError{
			Command: command,
			Code:    resp.Code,
			Message: errMsg,
			Details: resp.Details,
		}
	}

	return resp, nil
}

// sanitizeStderr strips sensitive data (tokens, paths, session info) from
// subprocess stderr before surfacing it in error messages.
func sanitizeStderr(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Cap length to avoid leaking large debug output
	const maxLen = 256
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	// Redact anything that looks like a token, session ID, or bearer header
	for _, pattern := range []string{"Bearer ", "token=", "session=", "AccessToken", "RefreshToken", "UID:"} {
		if idx := strings.Index(s, pattern); idx >= 0 {
			// Truncate from the sensitive prefix onward
			s = s[:idx] + "[redacted]"
			break
		}
	}
	return s
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// parseBridgeEnvelope validates and decodes the strict bridge response envelope.
func parseBridgeEnvelope(raw []byte) (*BridgeResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("bridge response must be a JSON object")
	}

	for key := range fields {
		switch key {
		case "ok", "payload", "error", "code", "details":
		default:
			return nil, fmt.Errorf("bridge response contains unknown field %q", key)
		}
	}

	rawOK, ok := fields["ok"]
	if !ok {
		return nil, fmt.Errorf("bridge response missing required ok field")
	}
	if isJSONNull(rawOK) {
		return nil, fmt.Errorf("bridge response ok field must be boolean")
	}

	var resp BridgeResponse
	if err := json.Unmarshal(rawOK, &resp.OK); err != nil {
		return nil, fmt.Errorf("bridge response ok field must be boolean: %w", err)
	}

	if rawPayload, ok := fields["payload"]; ok && !isJSONNull(rawPayload) {
		resp.Payload = append(json.RawMessage(nil), rawPayload...)
	}
	if rawError, ok := fields["error"]; ok {
		if isJSONNull(rawError) {
			return nil, fmt.Errorf("bridge response error field must be string")
		}
		if err := json.Unmarshal(rawError, &resp.Error); err != nil {
			return nil, fmt.Errorf("bridge response error field must be string: %w", err)
		}
	}
	if rawCode, ok := fields["code"]; ok {
		if isJSONNull(rawCode) {
			return nil, fmt.Errorf("bridge response code field must be integer")
		}
		if err := json.Unmarshal(rawCode, &resp.Code); err != nil {
			return nil, fmt.Errorf("bridge response code field must be integer: %w", err)
		}
	}
	if rawDetails, ok := fields["details"]; ok {
		if isJSONNull(rawDetails) {
			return nil, fmt.Errorf("bridge response details field must be string")
		}
		if err := json.Unmarshal(rawDetails, &resp.Details); err != nil {
			return nil, fmt.Errorf("bridge response details field must be string: %w", err)
		}
	}

	if resp.OK {
		if strings.TrimSpace(resp.Error) != "" {
			return nil, fmt.Errorf("successful bridge response must not include an error message")
		}
		if resp.Code != 0 {
			return nil, fmt.Errorf("successful bridge response must not include an error code")
		}
		if resp.Details != "" {
			return nil, fmt.Errorf("successful bridge response must not include error details")
		}
		return &resp, nil
	}

	if strings.TrimSpace(resp.Error) == "" {
		return nil, fmt.Errorf("failed bridge response missing error message")
	}
	if resp.Code <= 0 {
		return nil, fmt.Errorf("failed bridge response missing positive error code")
	}
	if len(resp.Payload) > 0 {
		return nil, fmt.Errorf("failed bridge response must not include payload")
	}

	return &resp, nil
}

// parseBridgeOutput extracts a JSON envelope from stdout, tolerating non-JSON
// noise (e.g. debug logging) by scanning from the last line backwards.
func parseBridgeOutput(stdout, _ []byte) (*BridgeResponse, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty stdout from bridge subprocess")
	}

	// Try the entire stdout first (fast path)
	if resp, err := parseBridgeEnvelope(trimmed); err == nil {
		return resp, nil
	} else if json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid bridge response envelope: %w", err)
	}

	// Scan lines from end looking for a strict JSON response envelope.
	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		resp, err := parseBridgeEnvelope(line)
		if err == nil {
			return resp, nil
		}
		if json.Valid(line) {
			return nil, fmt.Errorf("invalid bridge response envelope: %w", err)
		}
	}

	return nil, fmt.Errorf("no valid JSON envelope found in bridge output")
}

// buildCredentials creates the credential portion of a bridge request.
// Always sends credentialProvider — proton-drive-cli resolves credentials locally.
func buildCredentials(creds OperationCredentials, storageBase, appVersion string, allowLogin ...bool) map[string]any {
	m := map[string]any{}
	if creds.CredentialProvider != "" {
		m["credentialProvider"] = creds.CredentialProvider
	}
	if creds.DataCredentialProvider != "" {
		m["dataCredentialProvider"] = creds.DataCredentialProvider
		if creds.DataCredentialHost != "" {
			m["dataCredentialHost"] = creds.DataCredentialHost
		}
	}
	if storageBase != "" {
		m["storageBase"] = storageBase
	}
	if appVersion != "" {
		m["appVersion"] = appVersion
	}
	if len(allowLogin) > 0 {
		m["allowLogin"] = allowLogin[0]
	}
	return m
}

// Authenticate runs `bridge auth` to establish a session with Proton Drive.
func (bc *BridgeClient) Authenticate(creds OperationCredentials) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	_, err := bc.runBridgeCommand("auth", req)
	return err
}

// AuthState runs `bridge auth-state` to inspect local auth readiness without
// credential resolution or Proton network calls.
func (bc *BridgeClient) AuthState(creds OperationCredentials) (*BridgeAuthStateResponse, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	resp, err := bc.runBridgeCommand("auth-state", req)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Payload) == 0 {
		return nil, fmt.Errorf("bridge auth-state returned empty payload")
	}
	var state BridgeAuthStateResponse
	if err := json.Unmarshal(resp.Payload, &state); err != nil {
		return nil, fmt.Errorf("failed to parse auth-state payload: %w", err)
	}
	if strings.TrimSpace(state.State) == "" {
		return nil, fmt.Errorf("bridge auth-state payload missing state")
	}
	if state.WillAttemptNetwork {
		return nil, fmt.Errorf("bridge auth-state must be offline-only")
	}
	return &state, nil
}

// InitLFSStorage runs `bridge init` to ensure the LFS storage folder exists.
func (bc *BridgeClient) InitLFSStorage(creds OperationCredentials) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	_, err := bc.runBridgeCommand("init", req)
	return err
}

// Upload runs `bridge upload` to encrypt and store a file in Proton Drive.
func (bc *BridgeClient) Upload(creds OperationCredentials, oid, filePath string) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	req["oid"] = oid
	req["path"] = filePath
	_, err := bc.runBridgeCommand("upload", req)
	return err
}

// Download runs `bridge download` to decrypt and retrieve a file from Proton Drive.
func (bc *BridgeClient) Download(creds OperationCredentials, oid, outputPath string) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	req["oid"] = oid
	req["outputPath"] = outputPath
	_, err := bc.runBridgeCommand("download", req)
	return err
}

// Exists runs `bridge exists` to check if an OID is already stored.
func (bc *BridgeClient) Exists(creds OperationCredentials, oid string) (bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	req["oid"] = oid
	resp, err := bc.runBridgeCommand("exists", req)
	if err != nil {
		// A 404 error means the object does not exist — not a failure.
		if strings.Contains(err.Error(), "[404]") || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	if resp == nil {
		return false, nil
	}
	// Parse payload for explicit exists flag
	var result struct {
		Exists bool `json:"exists"`
	}
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &result); err == nil {
			return result.Exists, nil
		}
	}
	// If the command succeeded, the object exists
	return true, nil
}

// BatchExists runs `bridge batch-exists` for multiple OIDs.
func (bc *BridgeClient) BatchExists(creds OperationCredentials, oids []string) (map[string]bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	req["oids"] = oids
	resp, err := bc.runBridgeCommand("batch-exists", req)
	if err != nil {
		return nil, err
	}
	var result map[string]bool
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &result); err != nil {
			return nil, fmt.Errorf("failed to parse batch-exists response: %w", err)
		}
	}
	if result == nil {
		result = make(map[string]bool)
	}
	return result, nil
}

// BatchDelete runs `bridge batch-delete` for multiple OIDs.
func (bc *BridgeClient) BatchDelete(creds OperationCredentials, oids []string) (map[string]bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion, false)
	req["oids"] = oids
	resp, err := bc.runBridgeCommand("batch-delete", req)
	if err != nil {
		return nil, err
	}
	var result map[string]bool
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &result); err != nil {
			return nil, fmt.Errorf("failed to parse batch-delete response: %w", err)
		}
	}
	if result == nil {
		result = make(map[string]bool)
	}
	return result, nil
}

// resolveNodeBinary returns the path to the Node.js binary.
func resolveNodeBinary() string {
	if bin := os.Getenv("NODE_BIN"); bin != "" {
		return bin
	}
	if path, err := exec.LookPath("node"); err == nil {
		return path
	}
	return "node"
}
