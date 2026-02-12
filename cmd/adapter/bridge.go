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
		if resp.Code > 0 {
			return resp, fmt.Errorf("[%d] %s", resp.Code, errMsg)
		}
		return resp, fmt.Errorf("%s", errMsg)
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

// parseBridgeOutput extracts a JSON envelope from stdout, tolerating non-JSON
// noise (e.g. debug logging) by scanning from the last line backwards.
func parseBridgeOutput(stdout, _ []byte) (*BridgeResponse, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty stdout from bridge subprocess")
	}

	// Try the entire stdout first (fast path)
	var resp BridgeResponse
	if err := json.Unmarshal(trimmed, &resp); err == nil {
		return &resp, nil
	}

	// Scan lines from end looking for a JSON object
	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var lineResp BridgeResponse
		if err := json.Unmarshal(line, &lineResp); err == nil {
			return &lineResp, nil
		}
	}

	return nil, fmt.Errorf("no valid JSON envelope found in bridge output")
}

// buildCredentials creates the credential portion of a bridge request.
func buildCredentials(creds OperationCredentials, storageBase, appVersion string) map[string]any {
	m := map[string]any{}
	if creds.CredentialProvider != "" {
		m["credentialProvider"] = creds.CredentialProvider
	} else {
		if creds.Username != "" {
			m["username"] = creds.Username
		}
		if creds.Password != "" {
			m["password"] = creds.Password
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

// Authenticate runs `bridge auth` to establish a session with Proton Drive.
func (bc *BridgeClient) Authenticate(creds OperationCredentials) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	_, err := bc.runBridgeCommand("auth", req)
	return err
}

// InitLFSStorage runs `bridge init` to ensure the LFS storage folder exists.
func (bc *BridgeClient) InitLFSStorage(creds OperationCredentials) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	_, err := bc.runBridgeCommand("init", req)
	return err
}

// Upload runs `bridge upload` to encrypt and store a file in Proton Drive.
func (bc *BridgeClient) Upload(creds OperationCredentials, oid, filePath string) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oid"] = oid
	req["path"] = filePath
	_, err := bc.runBridgeCommand("upload", req)
	return err
}

// Download runs `bridge download` to decrypt and retrieve a file from Proton Drive.
func (bc *BridgeClient) Download(creds OperationCredentials, oid, outputPath string) error {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
	req["oid"] = oid
	req["outputPath"] = outputPath
	_, err := bc.runBridgeCommand("download", req)
	return err
}

// Exists runs `bridge exists` to check if an OID is already stored.
func (bc *BridgeClient) Exists(creds OperationCredentials, oid string) (bool, error) {
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
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
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
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
	req := buildCredentials(creds, bc.storageBase, bc.appVersion)
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
