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
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

const (
	Name                    = "git-lfs-proton-adapter"
	progressChunkSize int64 = 64 * 1024
	// defaultBridgeTimeout is the base wall-clock budget for one bridge
	// command; transfers add time proportional to the object size (see
	// BridgeClient.commandTimeout).
	defaultBridgeTimeout = 5 * time.Minute
)

var (
	// Populated by the build pipeline (-ldflags "-X main.Version=…").
	Version   = "dev"
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
	driveCLIBin string
	// logger carries per-message diagnostics and is silent unless --debug
	// is set; errLog always reaches stderr, which git-lfs records under
	// GIT_TRACE (tq/custom.go traceWriter).
	logger             *log.Logger
	errLog             *log.Logger
	session            *Session
	currentOperation   Direction
	allowMockTransfers bool
	localStoreDir      string
	backendKind        string
	backend            TransferBackend
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

// Session tracks the Git LFS transfer session started by the init message.
type Session struct {
	Initialized bool
	CreatedAt   time.Time
}

// NewAdapter creates a new adapter instance
func NewAdapter() *Adapter {
	adapter := &Adapter{
		logger:             log.New(os.Stderr, Name+": ", log.LstdFlags),
		errLog:             log.New(os.Stderr, Name+": ", log.LstdFlags),
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
				return nil
			}
			// custom-transfers.md, "Error handling": a protocol error is
			// reported on stdout and the process exits non-zero.
			message := "failed to decode message: " + err.Error()
			if sendErr := a.sendProtocolError(encoder, 1, message); sendErr != nil {
				return sendErr
			}
			return errors.New(message)
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

	progress := newTransferProgress(enc, normalizedOID)
	storedSize, err := a.uploadWithProgress(normalizedOID, msg.Path, sourceSize, progress.report)
	if err != nil {
		return a.sendBackendTransferError(enc, msg.OID, err)
	}

	if err := progress.finish(a, storedSize); err != nil {
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
	progress := newTransferProgress(enc, normalizedOID)
	stagedPath, stagedSize, err := a.downloadWithProgress(normalizedOID, msg.Size, progress.report)
	if err != nil {
		return a.sendBackendTransferError(enc, msg.OID, err)
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

	if err := progress.finish(a, stagedSize); err != nil {
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

// handleTerminate closes the transfer session. The status file keeps the
// last transfer result: git-lfs sends terminate after every session.
func (a *Adapter) handleTerminate(_ *InboundMessage, _ *json.Encoder) error {
	a.logger.Println("Terminating adapter")
	a.session = nil
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

// classifyError derives the status-file state, error code and recovery
// guidance of a failed transfer; a structured backend error code takes
// precedence over the HTTP-style status and the message.
func classifyError(code int, backendErrorCode ErrorCode, message string) (state string, errorCode string, errorDetail string) {
	if backendErrorCode != "" && backendErrorCode != ErrCodeUnknown {
		state, errorDetail = statusForErrorCode(backendErrorCode)
		return state, string(backendErrorCode), errorDetail
	}
	return classifyErrorByStatus(code, message)
}

// statusForErrorCode maps a structured ErrorCode to the status-file state and
// the recovery guidance shown by the tray.
func statusForErrorCode(errorCode ErrorCode) (state string, errorDetail string) {
	switch errorCode {
	case ErrCodeCaptchaRequired:
		return config.StateCaptcha, "Run: proton-drive-cli login to complete CAPTCHA verification"
	case ErrCodeRateLimited:
		return config.StateRateLimited, "Wait before retrying operations. The Proton API has rate-limited requests."
	case ErrCodeKeyPasswordRequired:
		return config.StateAuthRequired, "Run: proton-drive-cli login with the configured key-password provider"
	case ErrCodeAuthRequired:
		return config.StateAuthRequired, "Run: proton-drive-cli login to re-authenticate"
	case ErrCodeInsufficientScope:
		return config.StateError, "Proton rejected the app/session scope; run proton-lfs-cli scope-diagnostics and do not retry login"
	case ErrCodePermissionDenied:
		return config.StateError, "Proton Drive denied access to this object"
	case ErrCodeNotFound:
		return config.StateError, "The requested object was not found in Proton Drive"
	case ErrCodeNetworkFailure:
		return config.StateError, "Proton Drive is unreachable; the operation may be retried"
	case ErrCodeServerError:
		return config.StateError, "Proton Drive reported a server error; the operation may be retried"
	case ErrCodeInvalidRequest:
		return config.StateError, "Client error - check request parameters"
	default:
		return config.StateError, ""
	}
}

// classifyErrorByStatus classifies errors that carry no structured code from
// their HTTP-style status and message.
func classifyErrorByStatus(code int, message string) (state string, errorCode string, errorDetail string) {
	state = config.StateError
	errorCode = "unknown_error"
	errorDetail = ""

	if code == 407 || strings.Contains(message, "CAPTCHA") {
		state = config.StateCaptcha
		errorCode = "captcha_required"
		errorDetail = "Run: proton-drive-cli login to complete CAPTCHA verification"
		return
	}

	if code == 429 || strings.Contains(message, "Rate limit") || strings.Contains(message, "rate limit") {
		state = config.StateRateLimited
		errorCode = "rate_limited"
		errorDetail = "Wait before retrying operations. The Proton API has rate-limited requests."
		return
	}

	if code == 401 || code == 403 || strings.Contains(message, "Authentication") {
		state = config.StateAuthRequired
		errorCode = "auth_required"
		errorDetail = "Run: proton-drive-cli login to re-authenticate"
		return
	}

	switch code {
	case 404:
		errorCode = "not_found"
		errorDetail = "The requested resource was not found"
	case 409:
		errorCode = "conflict"
		errorDetail = "Resource conflict (e.g., hash or size mismatch)"
	case 500:
		errorCode = "server_error"
		errorDetail = "Internal server error - the operation may be retried"
	default:
		if code >= 500 && code < 600 {
			errorCode = "server_error"
			errorDetail = "Server error - the operation may be retried"
		} else if code >= 400 && code < 500 {
			errorCode = "client_error"
			errorDetail = "Client error - check request parameters"
		}
	}
	return
}

func (a *Adapter) sendTransferError(enc *json.Encoder, oid string, code int, message string) error {
	return a.sendTransferErrorWithMetadata(enc, oid, code, message, "", false, false)
}

func (a *Adapter) sendBackendTransferError(enc *json.Encoder, oid string, err error) error {
	code, message := backendErrorDetails(err)
	var backendErr *BackendError
	if errors.As(err, &backendErr) {
		return a.sendTransferErrorWithMetadata(enc, oid, code, message, backendErr.ErrorCode, backendErr.Retryable, backendErr.Temporary)
	}
	return a.sendTransferError(enc, oid, code, message)
}

func (a *Adapter) sendTransferErrorWithMetadata(enc *json.Encoder, oid string, code int, message string, backendErrorCode ErrorCode, retryable, temporary bool) error {
	a.errLog.Printf("Error [%d]: %s", code, message)

	state, errorCode, errorDetail := classifyError(code, backendErrorCode, message)

	_ = config.WriteStatus(config.StatusReport{
		State:       state,
		LastOID:     oid,
		LastOp:      string(a.currentOperation),
		Error:       message,
		ErrorCode:   errorCode,
		ErrorDetail: errorDetail,
		Retryable:   retryable,
		Temporary:   temporary,
	})

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
	a.errLog.Printf("Protocol error [%d]: %s", code, message)
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

// sendFinalProgress reports a transfer without streaming progress as one
// progress event for the whole object (custom-transfers.md, "Progress").
func (a *Adapter) sendFinalProgress(enc *json.Encoder, oid string, totalSize int64) error {
	if totalSize < 0 {
		totalSize = 0
	}
	return a.sendProgress(enc, oid, totalSize)
}

func (a *Adapter) uploadWithProgress(oid, sourcePath string, expectedSize int64, progress ProgressFunc) (int64, error) {
	if backend, ok := a.backend.(ProgressTransferBackend); ok {
		return backend.UploadWithProgress(a.session, oid, sourcePath, expectedSize, progress)
	}
	return a.backend.Upload(a.session, oid, sourcePath, expectedSize)
}

func (a *Adapter) downloadWithProgress(oid string, expectedSize int64, progress ProgressFunc) (string, int64, error) {
	if backend, ok := a.backend.(ProgressTransferBackend); ok {
		return backend.DownloadWithProgress(a.session, oid, expectedSize, progress)
	}
	return a.backend.Download(a.session, oid, expectedSize)
}

type transferProgress struct {
	enc        *json.Encoder
	oid        string
	bytesSoFar int64
	emitted    bool
}

func newTransferProgress(enc *json.Encoder, oid string) *transferProgress {
	return &transferProgress{enc: enc, oid: oid}
}

func (p *transferProgress) report(bytesSoFar, bytesSinceLast int64) error {
	if bytesSoFar < p.bytesSoFar {
		return fmt.Errorf("progress moved backwards: previous=%d current=%d", p.bytesSoFar, bytesSoFar)
	}
	if bytesSinceLast < 0 {
		return fmt.Errorf("progress increment is negative: %d", bytesSinceLast)
	}
	if expected := bytesSoFar - p.bytesSoFar; bytesSinceLast != expected {
		return fmt.Errorf("progress increment mismatch: got=%d expected=%d", bytesSinceLast, expected)
	}
	p.bytesSoFar = bytesSoFar
	p.emitted = true
	return p.enc.Encode(OutboundMessage{
		Event:      EventProgress,
		OID:        p.oid,
		BytesSoFar: bytesSoFar,
		BytesSince: bytesSinceLast,
	})
}

func (p *transferProgress) finish(a *Adapter, totalSize int64) error {
	if !p.emitted {
		return a.sendFinalProgress(p.enc, p.oid, totalSize)
	}
	if totalSize < p.bytesSoFar {
		return fmt.Errorf("progress total %d exceeds transferred size %d", p.bytesSoFar, totalSize)
	}
	if totalSize > p.bytesSoFar {
		return p.report(totalSize, totalSize-p.bytesSoFar)
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

	if err := a.sendFinalProgress(enc, strings.ToLower(msg.OID), info.Size()); err != nil {
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

	if err := a.sendFinalProgress(enc, strings.ToLower(msg.OID), msg.Size); err != nil {
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
	_, err := copyFileWithProgress(srcPath, dstPath, nil)
	return err
}

func copyFileWithProgress(srcPath, dstPath string, progress ProgressFunc) (int64, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".*")
	if err != nil {
		return 0, err
	}
	tmpPath := dst.Name()

	n, err := copyWithProgress(dst, src, progress)
	if err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return n, err
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpPath)
		return n, err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return n, err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return n, err
	}
	return n, nil
}

func copyIntoOpenFile(srcPath string, dst *os.File) error {
	_, err := copyIntoOpenFileWithProgress(srcPath, dst, nil)
	return err
}

func copyIntoOpenFileWithProgress(srcPath string, dst *os.File, progress ProgressFunc) (int64, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = src.Close() }()

	n, err := copyWithProgress(dst, src, progress)
	if err != nil {
		return n, err
	}
	return n, dst.Sync()
}

// progressWriter counts bytes written through it and reports each write to
// the progress callback.
type progressWriter struct {
	w        io.Writer
	total    int64
	progress ProgressFunc
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.total += int64(n)
		if p.progress != nil {
			if perr := p.progress(p.total, int64(n)); perr != nil {
				return n, perr
			}
		}
	}
	return n, err
}

// copyWithProgress copies src to dst in progressChunkSize reads. The reader
// is wrapped so that io.CopyBuffer cannot bypass the buffer through
// io.WriterTo, which keeps the progress granularity deterministic.
func copyWithProgress(dst io.Writer, src io.Reader, progress ProgressFunc) (int64, error) {
	pw := &progressWriter{w: dst, progress: progress}
	n, err := io.CopyBuffer(pw, struct{ io.Reader }{src}, make([]byte, progressChunkSize))
	return n, err
}

// createTempFile creates a temporary file for mock downloads.
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

// printUsage writes the adapter's help text to w; main installs it as flag.Usage.
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
      - progress reporting (local backend streams during copy; the sdk
        backend reports the whole object once the bridge command returns)
      - complete with per-object error handling
      - terminate (session state discarded)
      - standalone mode (action: null, no batch API)
      - concurrent instances (git-lfs spawns multiple adapter processes)

    Not implemented:
      - SDK real-time streaming progress (SDK progress is post-transfer)
      - Resume/retry on transient failure
      - Verify action (not required per spec)

BACKENDS
    local   Filesystem object store (default). No authentication.
            Objects stored at: <store-dir>/<oid[0:2]>/<oid[2:4]>/<oid>
    sdk     Proton Drive via proton-drive-cli subprocess.
            Objects stored at: /LFS/<oid[0:2]>/<oid[2:4]>/<oid>
            Upload deduplication via existence check before transfer.

AUTHENTICATION (sdk backend only)
    Run proton-drive-cli login (browser fork) before Git LFS transfers. Each
    transfer session first runs the bridge auth-state command and refuses
    every state other than ready and session_expired; bridge init refreshes
    an expired access token.

SECURITY
    - SHA-256 verification on upload and download
    - OID validation: /^[a-f0-9]{64}$/i
    - Path traversal prevention (.. segments, null bytes rejected)
    - No account username or password is accepted by transfer commands
    - Bridge requests passed as stdin JSON, never as command-line arguments
    - Subprocess environment filtered via allowlist
    - Bridge command timeout: --bridge-timeout (default 5m) plus one second
      per 256 KiB of object size for uploads and downloads

FLAGS
`)
	flag.CommandLine.SetOutput(w)
	flag.CommandLine.PrintDefaults()
	_, _ = fmt.Fprint(w, `
ENVIRONMENT VARIABLES
    PROTON_LFS_BACKEND             Backend: local or sdk (default: local)
    PROTON_LFS_LOCAL_STORE_DIR     Local store directory
    PROTON_DRIVE_CLI_BIN           proton-drive-cli entry point (dist/index.js
                                   run with node, or the standalone executable)
    PROTON_LFS_BRIDGE_TIMEOUT      Base bridge command timeout (Go duration,
                                   default 5m)
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
`)
}

func main() {
	defaultDriveCLIBin := envOrDefault(EnvDriveCLIBin, DefaultDriveCLIBin)
	driveCLIBin := flag.String("drive-cli-bin", defaultDriveCLIBin, "proton-drive-cli entry point: a .js file run with node, or the standalone executable")
	defaultBackend := envTrim(EnvBackend)
	if defaultBackend == "" {
		defaultBackend = BackendLocal
	}
	backend := flag.String("backend", defaultBackend, "Transfer backend to use: local or sdk")
	allowMockTransfers := flag.Bool("allow-mock-transfers", envBoolOrDefault(EnvAllowMockTransfers, false), "Allow mock upload/download behavior (simulation only)")
	localStoreDir := flag.String("local-store-dir", envTrim(EnvLocalStoreDir), "Local object store directory used for standalone transfers")
	bridgeTimeout := flag.Duration("bridge-timeout", defaultBridgeTimeout, "Base timeout for one proton-drive-cli bridge command; transfers add time proportional to the object size")
	debug := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Print version information")
	flag.Usage = func() { printUsage(os.Stderr) }
	if raw := envTrim(EnvBridgeTimeout); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			fmt.Fprintf(os.Stderr, "invalid %s %q: expected a positive Go duration such as 10m\n", EnvBridgeTimeout, raw)
			os.Exit(2)
		}
		*bridgeTimeout = parsed
	}
	flag.Parse()
	if *bridgeTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "invalid --bridge-timeout: expected a positive duration")
		os.Exit(2)
	}

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

	switch adapter.backendKind {
	case BackendLocal:
		adapter.backend = NewLocalStoreBackend(adapter.localStoreDir)
	case BackendSDK:
		bridgeCfg := BridgeClientConfig{
			CLIBin:      adapter.driveCLIBin,
			Timeout:     *bridgeTimeout,
			StorageBase: envOrDefault(EnvStorageBase, DefaultStorageBase),
			AppVersion:  envTrim(EnvAppVersion),
		}
		bridge := NewBridgeClient(bridgeCfg)
		adapter.backend = NewDriveCLIBackend(bridge)
	default:
		fmt.Fprintf(os.Stderr, "invalid backend %q (supported: local, sdk)\n", adapter.backendKind)
		os.Exit(2)
	}

	if !*debug {
		adapter.logger.SetOutput(io.Discard)
	}

	if removed := cleanupStaleTempFiles(10 * time.Minute); removed > 0 {
		adapter.logger.Printf("Cleaned up %d stale temp files", removed)
	}

	terminateBridgeOnSignal(adapter)

	if err := adapter.Run(os.Stdin, os.Stdout); err != nil && err != io.EOF {
		adapter.errLog.Fatalf("Adapter error: %v", err)
	}
}

// terminateBridgeOnSignal kills a running bridge subprocess before the
// adapter exits on SIGINT or SIGTERM, and exits with the conventional status
// 128+signal (130 for SIGINT, 143 for SIGTERM).
func terminateBridgeOnSignal(adapter *Adapter) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		if backend, ok := adapter.backend.(interface{ TerminateBridge() }); ok {
			backend.TerminateBridge()
		}
		code := 130
		if sig == syscall.SIGTERM {
			code = 143
		}
		os.Exit(code)
	}()
}
