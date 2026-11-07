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

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/bridgeproto"
)

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

// BridgeAuthStateResponse is the `bridge auth-state` payload
// (schemas/bridge/v1/auth-state-payload.schema.json): no token refresh and no
// Proton Drive request; the credentials check reads the session's credential store.
type BridgeAuthStateResponse struct {
	State                string   `json:"state"`
	HasSession           bool     `json:"hasSession"`
	SessionValid         bool     `json:"sessionValid"`
	SessionExpired       bool     `json:"sessionExpired"`
	SessionUIDPresent    bool     `json:"sessionUidPresent"`
	CredentialsAvailable bool     `json:"credentialsAvailable,omitempty"`
	CredentialStore      string   `json:"credentialStore,omitempty"`
	CredentialHost       string   `json:"credentialHost,omitempty"`
	WillAttemptNetwork   bool     `json:"willAttemptNetwork"`
	Errors               []string `json:"errors"`
	Actions              []string `json:"actions"`
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

// transferMinBytesPerSecond is the throughput floor for uploads and
// downloads: an object may take timeout + size/transferMinBytesPerSecond
// before the bridge is killed.
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

// BridgeStartError reports a bridge subprocess that could not be started
// (os/exec Cmd.Start): a missing, non-executable or unloadable program.
type BridgeStartError struct {
	Command string
	Err     error
}

func (e *BridgeStartError) Error() string {
	return fmt.Sprintf("bridge %s could not start: %v", e.Command, e.Err)
}

func (e *BridgeStartError) Unwrap() error { return e.Err }

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

// filteredEnv is the allowlisted process environment plus the configured
// extra variables.
func (bc *BridgeClient) filteredEnv() []string {
	return append(bridgeproto.FilterEnv(os.Environ()), bc.extraEnv...)
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

// commandArgv runs a JavaScript entry point (.js/.cjs/.mjs) under node and
// any other file, such as the single executable application, directly.
func (bc *BridgeClient) commandArgv(command string) (string, []string) {
	switch strings.ToLower(filepath.Ext(bc.cliBin)) {
	case ".js", ".cjs", ".mjs":
		return bc.nodeBin, []string{bc.cliBin, "bridge", command}
	default:
		return bc.cliBin, []string{"bridge", command}
	}
}

// runBridgeCommand executes a proton-drive-cli bridge command as a subprocess.
func (bc *BridgeClient) runBridgeCommand(command string, request map[string]any) (*bridgeproto.Response, error) {
	return bc.runBridgeCommandSized(command, request, 0)
}

// runBridgeCommandSized executes a bridge command whose timeout is scaled by
// the size of the object it transfers.
func (bc *BridgeClient) runBridgeCommandSized(command string, request map[string]any, transferSize int64) (*bridgeproto.Response, error) {
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

	if err := cmd.Start(); err != nil {
		return nil, &BridgeStartError{Command: command, Err: err}
	}
	bc.mu.Lock()
	bc.active = cmd
	bc.mu.Unlock()
	err = cmd.Wait()
	bc.mu.Lock()
	bc.active = nil
	bc.mu.Unlock()

	resp, parseErr := bridgeproto.ParseOutput(stdout.Bytes())
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

// sanitizeStderr redacts subprocess stderr and bounds it to 256 runes before
// it reaches an error message; redaction runs first so that the cut cannot
// split a secret out of its pattern.
func sanitizeStderr(raw string) string {
	return bridgeproto.Truncate(bridgeproto.RedactText(bridgeproto.Truncate(strings.TrimSpace(raw), 64*1024)), 256)
}

// baseRequest returns the request fields common to every bridge command
// (schemas/bridge/v1/request-field-rules.json).
func (bc *BridgeClient) baseRequest() map[string]any {
	m := map[string]any{}
	if bc.storageBase != "" {
		m["storageBase"] = bc.storageBase
	}
	if bc.appVersion != "" {
		m["appVersion"] = bc.appVersion
	}
	return m
}

// AuthState runs `bridge auth-state`, which checks the saved session and its
// credential-store entry without refreshing tokens or calling the Proton Drive API.
func (bc *BridgeClient) AuthState() (*BridgeAuthStateResponse, error) {
	req := bc.baseRequest()
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
func (bc *BridgeClient) InitLFSStorage() error {
	req := bc.baseRequest()
	_, err := bc.runBridgeCommand("init", req)
	return err
}

// Upload runs `bridge upload` to encrypt and store a file in Proton Drive.
// size is the object size announced by git-lfs and extends the timeout.
func (bc *BridgeClient) Upload(oid, filePath string, size int64) error {
	req := bc.baseRequest()
	req["oid"] = oid
	req["path"] = filePath
	_, err := bc.runBridgeCommandSized("upload", req, size)
	return err
}

// Download runs `bridge download` to decrypt and retrieve a file from Proton
// Drive. size is the object size announced by git-lfs and extends the timeout.
func (bc *BridgeClient) Download(oid, outputPath string, size int64) error {
	req := bc.baseRequest()
	req["oid"] = oid
	req["outputPath"] = outputPath
	_, err := bc.runBridgeCommandSized("download", req, size)
	return err
}

// Exists runs `bridge exists` to check if an OID is already stored.
func (bc *BridgeClient) Exists(oid string) (bool, error) {
	req := bc.baseRequest()
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
