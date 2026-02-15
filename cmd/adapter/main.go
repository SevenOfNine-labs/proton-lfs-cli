package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"proton-git-lfs/internal/config"
)

const (
	Version                 = "1.0.1"
	Name                    = "git-lfs-proton-adapter"
	progressChunkSize int64 = 64 * 1024
)

var (
	// Populated by build pipeline for release artifacts.
	GitCommit = "dev"
	BuildTime = "unknown"

	oidPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Event types (from Git LFS custom transfer protocol)
const (
	EventInit      = "init"
	EventUpload    = "upload"
	EventDownload  = "download"
	EventProgress  = "progress"
	EventComplete  = "complete"
	EventTerminate = "terminate"
)

// Direction of transfer operation
type Direction string

const (
	DirectionUpload   Direction = "upload"
	DirectionDownload Direction = "download"
)

// Adapter manages the transfer session with Git LFS
type Adapter struct {
	driveCLIBin        string
	logger             *log.Logger
	session            *Session
	currentOperation   Direction
	allowMockTransfers bool
	localStoreDir      string
	backendKind        string
	backend            TransferBackend
	credentialProvider string
}

// Message received from Git LFS
type InboundMessage struct {
	Event               string     `json:"event"`
	Operation           Direction  `json:"operation,omitempty"`
	Remote              string     `json:"remote,omitempty"`
	Concurrent          bool       `json:"concurrent,omitempty"`
	ConcurrentTransfers int        `json:"concurrenttransfers,omitempty"`
	OID                 string     `json:"oid,omitempty"`
	Size                int64      `json:"size,omitempty"`
	Path                string     `json:"path,omitempty"`
	Action              *ActionSet `json:"action,omitempty"`
}

// Message sent to Git LFS
type OutboundMessage struct {
	Event      string     `json:"event,omitempty"`
	OID        string     `json:"oid,omitempty"`
	Path       string     `json:"path,omitempty"`
	BytesSoFar int64      `json:"bytesSoFar,omitempty"`
	BytesSince int64      `json:"bytesSinceLast,omitempty"`
	Error      *ErrorInfo `json:"error,omitempty"`
}

// ActionSet contains transfer metadata from Git LFS batch API
type ActionSet struct {
	Href      string            `json:"href,omitempty"`
	ExpiresAt string            `json:"expiresAt,omitempty"`
	Header    map[string]string `json:"header,omitempty"`
}

// ErrorInfo represents an error response
type ErrorInfo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Session manages authentication with Proton Drive
type Session struct {
	Initialized bool
	Token       string
	CreatedAt   time.Time
}

// NewAdapter creates a new adapter instance
func NewAdapter() *Adapter {
	adapter := &Adapter{
		logger:             log.New(os.Stderr, Name+": ", log.LstdFlags),
		currentOperation:   "",
		allowMockTransfers: false,
		localStoreDir:      envTrim(EnvLocalStoreDir),
		backendKind:        BackendLocal,
	}
	adapter.backend = NewLocalStoreBackend(adapter.localStoreDir)
	return adapter
}

// Run starts the adapter's main message loop
func (a *Adapter) Run(r io.Reader, w io.Writer) error {
	decoder := json.NewDecoder(r)
	encoder := json.NewEncoder(w)

	for {
		var msg InboundMessage
		err := decoder.Decode(&msg)
		if err != nil {
			if err == io.EOF {
				return nil // Clean shutdown
			}
			return a.sendProtocolError(encoder, 1, "failed to decode message: "+err.Error())
		}

		if err := a.handleMessage(&msg, encoder); err != nil {
			a.logger.Printf("Error handling message: %v", err)
			return err
		}
	}
}

// handleMessage processes a single message from Git LFS
func (a *Adapter) handleMessage(msg *InboundMessage, enc *json.Encoder) error {
	switch msg.Event {
	case EventInit:
		return a.handleInit(msg, enc)
	case EventUpload:
		return a.handleUpload(msg, enc)
	case EventDownload:
		return a.handleDownload(msg, enc)
	case EventTerminate:
		return a.handleTerminate(msg, enc)
	default:
		return a.sendProtocolError(enc, 400, "unknown event: "+msg.Event)
	}
}

// handleInit initializes the transfer session
func (a *Adapter) handleInit(msg *InboundMessage, enc *json.Encoder) error {
	a.logger.Printf("Initializing adapter for %s operation", msg.Operation)

	if msg.Operation != DirectionUpload && msg.Operation != DirectionDownload {
		return a.sendProtocolError(enc, 400, "invalid operation for init")
	}

	a.currentOperation = msg.Operation

	// Initialize session with Proton LFS bridge
	a.session = &Session{
		Initialized: true,
		CreatedAt:   time.Now(),
	}

	if a.allowMockTransfers {
		return enc.Encode(OutboundMessage{})
	}

	if a.backend == nil {
		return a.sendProtocolError(enc, 500, "transfer backend is not configured")
	}
	if err := a.backend.Initialize(a.session); err != nil {
		a.session = nil
		code, message := backendErrorDetails(err)
		return a.sendProtocolError(enc, code, message)
	}

	_ = config.WriteStatus(config.StatusReport{State: config.StateIdle, LastOp: "init"})
	// Send empty response to indicate success
	return enc.Encode(OutboundMessage{})
}

// handleUpload processes a file upload request
func (a *Adapter) handleUpload(msg *InboundMessage, enc *json.Encoder) error {
	a.logger.Printf("Upload request: OID=%s Size=%d Path=%s", msg.OID, msg.Size, msg.Path)

	if err := a.validateTransferRequest(msg, true); err != nil {
		return a.sendTransferError(enc, msg.OID, 400, err.Error())
	}

	if a.allowMockTransfers {
		return a.handleMockUpload(msg, enc)
	}

	if a.backend == nil {
		return a.sendTransferError(enc, msg.OID, 500, "transfer backend is not configured")
	}

	normalizedOID := strings.ToLower(msg.OID)
	hash, sourceSize, err := calculateFileSHA256(msg.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a.sendTransferError(enc, msg.OID, 404, "upload source file not found")
		}
		return a.sendTransferError(enc, msg.OID, 500, "failed to read upload source file")
	}
	if msg.Size > 0 && sourceSize != msg.Size {
		return a.sendTransferError(enc, msg.OID, 409, "upload size does not match transfer request")
	}
	if hash != normalizedOID {
		return a.sendTransferError(enc, msg.OID, 409, "upload content hash does not match oid")
	}

	storedSize, err := a.backend.Upload(a.session, normalizedOID, msg.Path, sourceSize)
	if err != nil {
		code, message := backendErrorDetails(err)
		return a.sendTransferError(enc, msg.OID, code, message)
	}

	if err := a.sendProgressSequence(enc, normalizedOID, storedSize); err != nil {
		return err
	}

	_ = config.WriteStatus(config.StatusReport{State: config.StateOK, LastOID: normalizedOID, LastOp: "upload"})
	return enc.Encode(OutboundMessage{
		Event: EventComplete,
		OID:   normalizedOID,
	})
}

// handleDownload processes a file download request
func (a *Adapter) handleDownload(msg *InboundMessage, enc *json.Encoder) error {
	a.logger.Printf("Download request: OID=%s Size=%d", msg.OID, msg.Size)

	if err := a.validateTransferRequest(msg, false); err != nil {
		return a.sendTransferError(enc, msg.OID, 400, err.Error())
	}

	if a.allowMockTransfers {
		return a.handleMockDownload(msg, enc)
	}

	if a.backend == nil {
		return a.sendTransferError(enc, msg.OID, 500, "transfer backend is not configured")
	}

	normalizedOID := strings.ToLower(msg.OID)
	stagedPath, stagedSize, err := a.backend.Download(a.session, normalizedOID)
	if err != nil {
		code, message := backendErrorDetails(err)
		return a.sendTransferError(enc, msg.OID, code, message)
	}

	objectHash, objectSize, err := calculateFileSHA256(stagedPath)
	if err != nil {
		_ = os.Remove(stagedPath)
		return a.sendTransferError(enc, msg.OID, 500, "failed to validate downloaded object")
	}
	if objectHash != normalizedOID {
		_ = os.Remove(stagedPath)
		return a.sendTransferError(enc, msg.OID, 500, "downloaded object hash mismatch")
	}
	if msg.Size > 0 && objectSize != msg.Size {
		_ = os.Remove(stagedPath)
		return a.sendTransferError(enc, msg.OID, 409, "downloaded object size does not match transfer request")
	}
	if stagedSize != objectSize {
		stagedSize = objectSize
	}

	if err := a.sendProgressSequence(enc, normalizedOID, stagedSize); err != nil {
		_ = os.Remove(stagedPath)
		return err
	}

	_ = config.WriteStatus(config.StatusReport{State: config.StateOK, LastOID: normalizedOID, LastOp: "download"})
	return enc.Encode(OutboundMessage{
		Event: EventComplete,
		OID:   normalizedOID,
		Path:  stagedPath,
	})
}

// handleTerminate closes the transfer session
func (a *Adapter) handleTerminate(_ *InboundMessage, _ *json.Encoder) error {
	a.logger.Println("Terminating adapter")
	a.session = nil
	_ = config.WriteStatus(config.StatusReport{State: config.StateIdle, LastOp: "terminate"})
	return nil
}

func (a *Adapter) validateTransferRequest(msg *InboundMessage, requirePath bool) error {
	if a.session == nil || !a.session.Initialized {
		return errors.New("session not initialized")
	}
	if msg.Size < 0 {
		return errors.New("invalid transfer size")
	}
	if !oidPattern.MatchString(strings.ToLower(msg.OID)) {
		return errors.New("invalid oid format")
	}
	if requirePath {
		p := strings.TrimSpace(msg.Path)
		if p == "" {
			return errors.New("missing upload path")
		}
		if err := validateFilePath(p); err != nil {
			return err
		}
	}
	return nil
}

// validateFilePath rejects paths that contain null bytes or path traversal segments.
func validateFilePath(p string) error {
	if strings.ContainsRune(p, 0) {
		return errors.New("null bytes not allowed in path")
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return errors.New("path traversal not allowed")
		}
	}
	return nil
}

func (a *Adapter) sendTransferError(enc *json.Encoder, oid string, code int, message string) error {
	a.logger.Printf("Error [%d]: %s", code, message)
	_ = config.WriteStatus(config.StatusReport{State: config.StateError, Error: message})
	return enc.Encode(OutboundMessage{
		Event: EventComplete,
		OID:   oid,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	})
}

func (a *Adapter) sendProtocolError(enc *json.Encoder, code int, message string) error {
	a.logger.Printf("Protocol error [%d]: %s", code, message)
	return enc.Encode(OutboundMessage{
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	})
}

func (a *Adapter) sendProgress(enc *json.Encoder, oid string, size int64) error {
	return enc.Encode(OutboundMessage{
		Event:      EventProgress,
		OID:        oid,
		BytesSoFar: size,
		BytesSince: size,
	})
}

func (a *Adapter) sendProgressSequence(enc *json.Encoder, oid string, totalSize int64) error {
	if totalSize <= 0 {
		return a.sendProgress(enc, oid, 0)
	}

	var bytesSoFar int64
	for bytesSoFar < totalSize {
		nextBytes := bytesSoFar + progressChunkSize
		if nextBytes > totalSize {
			nextBytes = totalSize
		}
		if err := enc.Encode(OutboundMessage{
			Event:      EventProgress,
			OID:        oid,
			BytesSoFar: nextBytes,
			BytesSince: nextBytes - bytesSoFar,
		}); err != nil {
			return err
		}
		bytesSoFar = nextBytes
	}
	return nil
}

func (a *Adapter) localObjectPath(oid string) string {
	if len(oid) < 4 {
		return filepath.Join(a.localStoreDir, oid)
	}
	return filepath.Join(a.localStoreDir, oid[:2], oid[2:4], oid)
}

func (a *Adapter) handleMockUpload(msg *InboundMessage, enc *json.Encoder) error {
	info, err := os.Stat(msg.Path)
	if err != nil {
		return a.sendTransferError(enc, msg.OID, 404, "upload source file not found")
	}
	if msg.Size > 0 && info.Size() != msg.Size {
		return a.sendTransferError(enc, msg.OID, 409, "upload size does not match transfer request")
	}

	time.Sleep(100 * time.Millisecond)

	if err := a.sendProgressSequence(enc, strings.ToLower(msg.OID), info.Size()); err != nil {
		return err
	}

	return enc.Encode(OutboundMessage{
		Event: EventComplete,
		OID:   strings.ToLower(msg.OID),
	})
}

func (a *Adapter) handleMockDownload(msg *InboundMessage, enc *json.Encoder) error {
	tmpFile, err := a.createTempFile()
	if err != nil {
		return a.sendTransferError(enc, msg.OID, 500, "failed to create temp file: "+err.Error())
	}
	defer func() { _ = tmpFile.Close() }()

	if msg.Size > 0 {
		if err := tmpFile.Truncate(msg.Size); err != nil {
			return a.sendTransferError(enc, msg.OID, 500, "failed to allocate mock download file: "+err.Error())
		}
	}

	if err := a.sendProgressSequence(enc, strings.ToLower(msg.OID), msg.Size); err != nil {
		return err
	}

	return enc.Encode(OutboundMessage{
		Event: EventComplete,
		OID:   strings.ToLower(msg.OID),
		Path:  tmpFile.Name(),
	})
}

func calculateFileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	tmpPath := fmt.Sprintf("%s.tmp-%d", dstPath, time.Now().UnixNano())
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func copyIntoOpenFile(srcPath string, dst *os.File) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Sync()
}

// createTempFile creates a temporary file for downloads
func (a *Adapter) createTempFile() (*os.File, error) {
	return os.CreateTemp("", "git-lfs-proton-*")
}

// cleanupStaleTempFiles removes leftover temp files from previous adapter runs.
// Files older than the given threshold are considered stale (orphaned on crash).
func cleanupStaleTempFiles(maxAge time.Duration) int {
	tmpDir := os.TempDir()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "git-lfs-proton-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(tmpDir, name)) == nil {
				removed++
			}
		}
	}
	return removed
}

// printUsage writes the adapter's help text to w. It is assigned to flag.Usage
// so that --help produces a comprehensive reference instead of a bare flag list.
func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `NAME
    git-lfs-proton-adapter - Git LFS custom transfer agent for Proton Drive

SYNOPSIS
    Invoked by git-lfs, not directly. Configure via git config:

    git config lfs.customtransfer.proton.path  /path/to/git-lfs-proton-adapter
    git config lfs.customtransfer.proton.args  "--backend sdk"
    git config lfs.standalonetransferagent     proton

DESCRIPTION
    Standalone custom transfer agent implementing the Git LFS custom transfer
    protocol. Communicates with git-lfs via line-delimited JSON on stdin/stdout.
    No batch API server required.

    Transfers files to/from Proton Drive with end-to-end encryption via
    proton-drive-cli subprocess, or to a local filesystem for testing.

PROTOCOL COMPLIANCE (submodules/git-lfs/docs/custom-transfers.md)
    Implemented:
      - init (upload/download operation)
      - upload with SHA-256 integrity verification
      - download with temp file path return
      - progress reporting (64KB chunks)
      - complete with per-object error handling
      - terminate with credential zeroing
      - standalone mode (action: null, no batch API)
      - concurrent instances (git-lfs spawns multiple adapter processes)

    Not implemented:
      - Real-time streaming progress (progress is post-transfer)
      - Resume/retry on transient failure
      - Verify action (not required per spec)

BACKENDS
    local   Filesystem object store (default). No authentication.
            Objects stored at: <store-dir>/<oid[0:2]>/<oid[2:4]>/<oid>
    sdk     Proton Drive via proton-drive-cli subprocess.
            Objects stored at: /LFS/<oid[0:2]>/<oid[2:4]>/<oid>
            Upload deduplication via existence check before transfer.

CREDENTIAL PROVIDERS (sdk backend only)
    pass-cli (default)
            Credentials resolved by proton-drive-cli via Proton Pass CLI.
            Setup: pass-cli login && proton-drive credential store --provider pass-cli
    git-credential
            Credentials resolved by proton-drive-cli via git credential fill.
            Setup: proton-drive credential store -u <email>

SECURITY
    - SHA-256 verification on upload and download
    - OID validation: /^[a-f0-9]{64}$/i
    - Path traversal prevention (.. segments, null bytes rejected)
    - Credentials passed via stdin JSON (not visible in ps)
    - Credential buffers zeroed on terminate
    - Subprocess environment filtered via allowlist
    - Subprocess concurrency limit: 10 max, 5-min timeout

FLAGS
`)
	flag.CommandLine.SetOutput(w)
	flag.CommandLine.PrintDefaults()
	_, _ = fmt.Fprint(w, `
ENVIRONMENT VARIABLES
    PROTON_LFS_BACKEND             Backend: local or sdk (default: local)
    PROTON_LFS_LOCAL_STORE_DIR     Local store directory
    PROTON_CREDENTIAL_PROVIDER     Credential provider: pass-cli or git-credential
    PROTON_DRIVE_CLI_BIN           proton-drive-cli path
    NODE_BIN                       Node.js binary path
    LFS_STORAGE_BASE               Remote storage base folder (default: LFS)
    PROTON_APP_VERSION             Proton API app version header
    ADAPTER_ALLOW_MOCK_TRANSFERS   Allow mock mode (default: false)

EXAMPLES
    # Local backend (testing)
    git config lfs.customtransfer.proton.path  ./bin/git-lfs-proton-adapter
    git config lfs.customtransfer.proton.args  "--backend local --local-store-dir /tmp/lfs"
    git config lfs.standalonetransferagent     proton

    # Proton Drive with pass-cli
    git config lfs.customtransfer.proton.path  ./bin/git-lfs-proton-adapter
    git config lfs.customtransfer.proton.args  "--backend sdk"
    git config lfs.standalonetransferagent     proton

    # Proton Drive with git-credential
    git config lfs.customtransfer.proton.path  ./bin/git-lfs-proton-adapter
    git config lfs.customtransfer.proton.args  "--backend sdk --credential-provider git-credential"
    git config lfs.standalonetransferagent     proton
`)
}

func main() {
	defaultDriveCLIBin := envOrDefault(EnvDriveCLIBin, DefaultDriveCLIBin)
	driveCLIBin := flag.String("drive-cli-bin", defaultDriveCLIBin, "Path to proton-drive-cli dist/index.js")
	defaultBackend := envTrim(EnvBackend)
	if defaultBackend == "" {
		defaultBackend = BackendLocal
	}
	backend := flag.String("backend", defaultBackend, "Transfer backend to use: local or sdk")
	allowMockTransfers := flag.Bool("allow-mock-transfers", envBoolOrDefault(EnvAllowMockTransfers, false), "Allow mock upload/download behavior (simulation only)")
	localStoreDir := flag.String("local-store-dir", envTrim(EnvLocalStoreDir), "Local object store directory used for standalone transfers")
	defaultCredProvider := envOrDefault(EnvCredentialProvider, DefaultCredentialProvider)
	credentialProvider := flag.String("credential-provider", defaultCredProvider, "Credential provider: pass-cli (default) or git-credential")
	debug := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Print version information")
	flag.Usage = func() { printUsage(os.Stderr) }
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s (commit=%s build_time=%s)\n", Name, Version, GitCommit, BuildTime)
		return
	}

	adapter := NewAdapter()
	adapter.driveCLIBin = strings.TrimSpace(*driveCLIBin)
	adapter.allowMockTransfers = *allowMockTransfers
	adapter.localStoreDir = strings.TrimSpace(*localStoreDir)
	adapter.backendKind = strings.ToLower(strings.TrimSpace(*backend))
	if adapter.backendKind == "" {
		adapter.backendKind = BackendLocal
	}
	adapter.credentialProvider = strings.ToLower(strings.TrimSpace(*credentialProvider))
	if adapter.credentialProvider == "" {
		adapter.credentialProvider = DefaultCredentialProvider
	}

	switch adapter.backendKind {
	case BackendLocal:
		adapter.backend = NewLocalStoreBackend(adapter.localStoreDir)
	case BackendSDK:
		bridgeCfg := BridgeClientConfig{
			CLIBin:      adapter.driveCLIBin,
			StorageBase: envOrDefault(EnvStorageBase, DefaultStorageBase),
			AppVersion:  envTrim(EnvAppVersion),
		}
		bridge := NewBridgeClient(bridgeCfg)
		adapter.backend = NewDriveCLIBackend(bridge, adapter.credentialProvider)
	default:
		fmt.Fprintf(os.Stderr, "invalid backend %q (supported: local, sdk)\n", adapter.backendKind)
		os.Exit(2)
	}

	if !*debug {
		adapter.logger.SetOutput(io.Discard)
	}

	// Remove stale temp files from previous adapter runs
	if removed := cleanupStaleTempFiles(10 * time.Minute); removed > 0 {
		adapter.logger.Printf("Cleaned up %d stale temp files", removed)
	}

	// Read from stdin, write to stdout
	if err := adapter.Run(os.Stdin, os.Stdout); err != nil && err != io.EOF {
		adapter.logger.Fatalf("Adapter error: %v", err)
	}
}
