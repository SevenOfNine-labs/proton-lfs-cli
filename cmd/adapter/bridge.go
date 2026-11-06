package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// BridgeError is the failed response envelope of a bridge command
// (schemas/bridge/v1/response-envelope.schema.json); Error() renders it as
// "[code] message".
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
	NodeBin     string
	CLIBin      string
	Timeout     time.Duration
	StorageBase string
	AppVersion  string
	ExtraEnv    []string // additional env vars (for testing)
}

// BridgeAuthStateResponse mirrors proton-drive-cli bridge auth-state payloads.
// It is intentionally local-only on the drive-cli side: no credential lookup,
// no token refresh, and no Proton network request.
type BridgeAuthStateResponse struct {
	State                  string   `json:"state"`
	HasSession             bool     `json:"hasSession"`
	SessionValid           bool     `json:"sessionValid"`
	SessionExpired         bool     `json:"sessionExpired"`
	SessionUIDPresent      bool     `json:"sessionUidPresent"`
	PasswordMode           int      `json:"passwordMode,omitempty"`
	AuthMode               string   `json:"authMode,omitempty"`
	KeyPasswordPersisted   bool     `json:"keyPasswordPersisted,omitempty"`
	KeyPasswordAvailable   bool     `json:"keyPasswordAvailable,omitempty"`
	KeyPasswordProvider    string   `json:"keyPasswordProvider,omitempty"`
	KeyPasswordHost        string   `json:"keyPasswordHost,omitempty"`
	DataCredentialProvider string   `json:"dataCredentialProvider,omitempty"`
	DataCredentialHost     string   `json:"dataCredentialHost,omitempty"`
	WillAttemptNetwork     bool     `json:"willAttemptNetwork"`
	Errors                 []string `json:"errors"`
	Actions                []string `json:"actions"`
}

// BridgeClient communicates with proton-drive-cli via subprocess stdin/stdout.
type BridgeClient struct {
	nodeBin     string
	cliBin      string
	timeout     time.Duration
	storageBase string
	appVersion  string
	extraEnv    []string

	// active is the running bridge subprocess, killed by TerminateActive
	// when the adapter itself is interrupted.
	mu     sync.Mutex
	active *exec.Cmd
}

// TerminateActive kills the running bridge subprocess and, on Unix, its
// process group. The bridge runs in its own group, so a terminal Ctrl-C that
// interrupts git, git-lfs and the adapter does not reach it directly.
func (bc *BridgeClient) TerminateActive() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if bc.active != nil && bc.active.Process != nil {
		_ = killBridgeProcess(bc.active)
	}
}

// transferMinBytesPerSecond is the throughput floor used to extend the base
// timeout for uploads and downloads: an object may take
// timeout + size/transferMinBytesPerSecond before the bridge is killed.
const transferMinBytesPerSecond = 256 * 1024

// maxBridgeCommandTimeout caps the size-derived budget; the object size comes
// from the LFS pointer, which a cloned repository controls.
const maxBridgeCommandTimeout = 24 * time.Hour

// BridgeTimeoutError reports a bridge command killed at its deadline.
type BridgeTimeoutError struct {
	Command string
	Timeout time.Duration
}

func (e *BridgeTimeoutError) Error() string {
	return fmt.Sprintf("bridge %s timed out after %s", e.Command, e.Timeout)
}

// NewBridgeClient creates a new bridge subprocess client.
func NewBridgeClient(cfg BridgeClientConfig) *BridgeClient {
	if cfg.NodeBin == "" {
		cfg.NodeBin = resolveNodeBinary()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultBridgeTimeout
	}
	if cfg.StorageBase == "" {
		cfg.StorageBase = DefaultStorageBase
	}
	return &BridgeClient{
		nodeBin:     cfg.NodeBin,
		cliBin:      cfg.CLIBin,
		timeout:     cfg.Timeout,
		storageBase: cfg.StorageBase,
		appVersion:  cfg.AppVersion,
		extraEnv:    cfg.ExtraEnv,
	}
}

// envAllowlist lists the environment variables forwarded to the bridge
// subprocess: entries ending in "_" are prefixes, all others exact names.
// Only variables proton-drive-cli reads are forwarded; secret-shaped names
// such as PROTON_PASSWORD or PROTON_DATA_PASSWORD never reach the bridge
// (docs/security/threat-model.md).
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
	// proton-drive-cli configuration (src/constants.ts,
	// src/auth/key-password-store.ts, src/credentials/pass-cli.ts).
	"PROTON_DRIVE_CLI_APP_VERSION",
	"PROTON_KEY_PASSWORD_PROVIDER",
	"PROTON_KEY_PASSWORD_HOST",
	"PROTON_DATA_CREDENTIAL_PROVIDER",
	"PROTON_DATA_CREDENTIAL_HOST",
	"PROTON_PASS_CLI_BIN",
	"PROTON_AUTH_TRACE",
	"PROTON_AUTH_TRACE_ID",
	"TMPDIR",
	"TMP",
	"TEMP",
	// Windows locates the user profile, git's global configuration and
	// system DLLs through these; they carry no secrets.
	"USERPROFILE",
	"HOMEDRIVE",
	"HOMEPATH",
	"APPDATA",
	"LOCALAPPDATA",
	"SYSTEMROOT",
	"WINDIR",
	"COMSPEC",
	"PATHEXT",
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

// commandTimeout returns the wall-clock budget for one bridge command moving
// transferSize bytes (0 for commands that move no payload).
func (bc *BridgeClient) commandTimeout(transferSize int64) time.Duration {
	if transferSize <= 0 {
		return bc.timeout
	}
	extra := transferSize / transferMinBytesPerSecond
	if extra >= int64(maxBridgeCommandTimeout/time.Second) {
		return maxBridgeCommandTimeout
	}
	total := bc.timeout + time.Duration(extra)*time.Second
	if total > maxBridgeCommandTimeout {
		return maxBridgeCommandTimeout
	}
	return total
}

// commandArgv returns the program and arguments for a bridge command. A
// JavaScript entry point (.js/.cjs/.mjs) runs under node; anything else is
// an executable such as the single-executable-application build and runs
// directly.
func (bc *BridgeClient) commandArgv(command string) (string, []string) {
	switch strings.ToLower(filepath.Ext(bc.cliBin)) {
	case ".js", ".cjs", ".mjs":
		return bc.nodeBin, []string{bc.cliBin, "bridge", command}
	default:
		return bc.cliBin, []string{"bridge", command}
	}
}

// runBridgeCommand executes a proton-drive-cli bridge command as a subprocess.
func (bc *BridgeClient) runBridgeCommand(command string, request map[string]any) (*BridgeResponse, error) {
	return bc.runBridgeCommandSized(command, request, 0)
}

// runBridgeCommandSized executes a bridge command whose timeout is scaled by
// the size of the object it transfers.
func (bc *BridgeClient) runBridgeCommandSized(command string, request map[string]any, transferSize int64) (*BridgeResponse, error) {
	timeout := bc.commandTimeout(transferSize)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	program, args := bc.commandArgv(command)
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = bc.filteredEnv()
	configureBridgeProcess(cmd)

	stdinBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bridge request: %w", err)
	}
	cmd.Stdin = bytes.NewReader(stdinBytes)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Start()
	if err == nil {
		bc.mu.Lock()
		bc.active = cmd
		bc.mu.Unlock()
		err = cmd.Wait()
		bc.mu.Lock()
		bc.active = nil
		bc.mu.Unlock()
	}

	resp, parseErr := parseBridgeOutput(stdout.Bytes(), stderr.Bytes())
	if parseErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, &BridgeTimeoutError{Command: command, Timeout: timeout}
		}
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

// parseBridgeOutput extracts the JSON envelope from stdout. The bridge
// contract allows only the envelope on stdout; the backwards line scan keeps
// the adapter working when a dependency of the CLI prints to stdout (Node
// runtime warnings), and still rejects output whose last object is not a
// valid envelope.
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

// buildCredentials creates the local unlock portion of a bridge request.
// Account authentication is browser-fork-only, so the root adapter never sends
// login credential selectors or login permission flags.
func buildCredentials(creds OperationCredentials, storageBase, appVersion string) map[string]any {
	m := map[string]any{}
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
	return m
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
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	_, err := bc.runBridgeCommand("init", req)
	return err
}

// Upload runs `bridge upload` to encrypt and store a file in Proton Drive.
// size is the object size announced by git-lfs and extends the timeout.
func (bc *BridgeClient) Upload(creds OperationCredentials, oid, filePath string, size int64) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oid"] = oid
	req["path"] = filePath
	_, err := bc.runBridgeCommandSized("upload", req, size)
	return err
}

// Download runs `bridge download` to decrypt and retrieve a file from Proton
// Drive. size is the object size announced by git-lfs and extends the timeout.
func (bc *BridgeClient) Download(creds OperationCredentials, oid, outputPath string, size int64) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oid"] = oid
	req["outputPath"] = outputPath
	_, err := bc.runBridgeCommandSized("download", req, size)
	return err
}

// Exists runs `bridge exists` to check if an OID is already stored.
func (bc *BridgeClient) Exists(creds OperationCredentials, oid string) (bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oid"] = oid
	resp, err := bc.runBridgeCommand("exists", req)
	if err != nil {
		// A 404 envelope means the object is absent, which is not a failure.
		var bridgeErr *BridgeError
		if errors.As(err, &bridgeErr) && bridgeErr.Code == 404 {
			return false, nil
		}
		return false, err
	}
	if resp == nil {
		return false, nil
	}
	return parseBridgeExistsPayload(resp.Payload)
}

// batchExists runs `bridge batch-exists` for multiple OIDs.
// It is an internal maintenance/helper surface, not part of the Git LFS custom
// transfer loop, which remains single-object per protocol message.
func (bc *BridgeClient) batchExists(creds OperationCredentials, oids []string) (map[string]bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oids"] = oids
	resp, err := bc.runBridgeCommand("batch-exists", req)
	if err != nil {
		return nil, err
	}
	return parseBridgeBoolMapPayload("batch-exists", resp.Payload)
}

// batchDelete runs `bridge batch-delete` for multiple OIDs.
// It is an internal maintenance/helper surface, not part of the Git LFS custom
// transfer loop, which remains single-object per protocol message.
func (bc *BridgeClient) batchDelete(creds OperationCredentials, oids []string) (map[string]bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oids"] = oids
	resp, err := bc.runBridgeCommand("batch-delete", req)
	if err != nil {
		return nil, err
	}
	return parseBridgeBoolMapPayload("batch-delete", resp.Payload)
}

func parseBridgeBoolMapPayload(command string, payload json.RawMessage) (map[string]bool, error) {
	var result map[string]bool
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil, fmt.Errorf("%s response missing payload", command)
	}

	if err := json.Unmarshal(payload, &result); err == nil {
		if result == nil {
			return map[string]bool{}, nil
		}
		if err := validateBridgeBoolMapKeys(command, result); err != nil {
			return nil, err
		}
		return result, nil
	}

	var wrapped struct {
		Results map[string]bool `json:"results"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, fmt.Errorf("failed to parse %s response: %w", command, err)
	}
	if wrapped.Results == nil {
		return nil, fmt.Errorf("%s response missing results", command)
	}
	if err := validateBridgeBoolMapKeys(command, wrapped.Results); err != nil {
		return nil, err
	}
	return wrapped.Results, nil
}

func parseBridgeExistsPayload(payload json.RawMessage) (bool, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return false, fmt.Errorf("exists response missing payload")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return false, fmt.Errorf("failed to parse exists response: %w", err)
	}
	if fields == nil {
		return false, fmt.Errorf("exists response must be an object")
	}
	for field := range fields {
		switch field {
		case "oid", "exists":
		default:
			return false, fmt.Errorf("exists response contains unknown field %q", field)
		}
	}

	rawExists, ok := fields["exists"]
	if !ok {
		return false, fmt.Errorf("exists response missing exists field")
	}
	var exists bool
	if err := json.Unmarshal(rawExists, &exists); err != nil {
		return false, fmt.Errorf("exists response exists field must be boolean: %w", err)
	}
	if rawOID, ok := fields["oid"]; ok {
		var oid string
		if err := json.Unmarshal(rawOID, &oid); err != nil {
			return false, fmt.Errorf("exists response oid field must be string: %w", err)
		}
		if !oidPattern.MatchString(strings.ToLower(oid)) {
			return false, fmt.Errorf("exists response oid field must be a 64-character hex string")
		}
	}
	return exists, nil
}

func validateBridgeBoolMapKeys(command string, results map[string]bool) error {
	for oid := range results {
		if !oidPattern.MatchString(strings.ToLower(oid)) {
			return fmt.Errorf("%s response contains invalid oid key %q", command, oid)
		}
	}
	return nil
}

// resolveNodeBinary returns the path to the Node.js binary.
func resolveNodeBinary() string {
	if bin := envTrim(EnvNodeBin); bin != "" {
		return bin
	}
	if path, err := exec.LookPath("node"); err == nil {
		return path
	}
	return "node"
}
