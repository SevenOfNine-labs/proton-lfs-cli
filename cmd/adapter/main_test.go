package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
)

const validOID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func decodeAllMessages(t *testing.T, data []byte) []OutboundMessage {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	out := make([]OutboundMessage, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var msg OutboundMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			t.Fatalf("failed to decode message: %v", err)
		}
		out = append(out, msg)
	}
	return out
}

func configureLocalBackend(adapter *Adapter, storeDir string) {
	adapter.localStoreDir = storeDir
	adapter.backendKind = BackendLocal
	adapter.backend = NewLocalStoreBackend(storeDir)
}

type observingBackend struct {
	uploadSize   int64
	downloadPath string
	downloadSize int64
	onUpload     func()
	onDownload   func()
}

func (b *observingBackend) Initialize(_ *Session) error {
	return nil
}

func (b *observingBackend) Upload(_ *Session, _, _ string, _ int64) (int64, error) {
	if b.onUpload != nil {
		b.onUpload()
	}
	return b.uploadSize, nil
}

func (b *observingBackend) Download(_ *Session, _ string) (string, int64, error) {
	if b.onDownload != nil {
		b.onDownload()
	}
	return b.downloadPath, b.downloadSize, nil
}

type streamingObservingBackend struct {
	uploadSize         int64
	downloadPath       string
	downloadSize       int64
	uploadStreamed     bool
	downloadStreamed   bool
	onUploadProgress   func()
	onDownloadProgress func()
}

func (b *streamingObservingBackend) Initialize(_ *Session) error {
	return nil
}

func (b *streamingObservingBackend) Upload(_ *Session, _, _ string, _ int64) (int64, error) {
	return b.uploadSize, nil
}

func (b *streamingObservingBackend) Download(_ *Session, _ string) (string, int64, error) {
	return b.downloadPath, b.downloadSize, nil
}

func (b *streamingObservingBackend) UploadWithProgress(_ *Session, _, _ string, _ int64, progress ProgressFunc) (int64, error) {
	b.uploadStreamed = true
	if err := progress(b.uploadSize, b.uploadSize); err != nil {
		return 0, err
	}
	if b.onUploadProgress != nil {
		b.onUploadProgress()
	}
	return b.uploadSize, nil
}

func (b *streamingObservingBackend) DownloadWithProgress(_ *Session, _ string, progress ProgressFunc) (string, int64, error) {
	b.downloadStreamed = true
	if err := progress(b.downloadSize, b.downloadSize); err != nil {
		return "", 0, err
	}
	if b.onDownloadProgress != nil {
		b.onDownloadProgress()
	}
	return b.downloadPath, b.downloadSize, nil
}

type progressCountingWriter struct {
	t      *testing.T
	count  int64
	last   int64
	latest OutboundMessage
}

func (w *progressCountingWriter) Write(p []byte) (int, error) {
	w.t.Helper()

	var msg OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(p), &msg); err != nil {
		w.t.Fatalf("failed to decode progress message: %v", err)
	}
	if msg.Event != EventProgress {
		w.t.Fatalf("expected progress event, got %+v", msg)
	}
	if msg.OID != validOID {
		w.t.Fatalf("expected oid %s, got %s", validOID, msg.OID)
	}
	if msg.BytesSoFar < w.last {
		w.t.Fatalf("progress moved backwards: previous=%d current=%d", w.last, msg.BytesSoFar)
	}
	if msg.BytesSince != msg.BytesSoFar-w.last {
		w.t.Fatalf("bytesSinceLast=%d, want %d", msg.BytesSince, msg.BytesSoFar-w.last)
	}
	w.last = msg.BytesSoFar
	w.latest = msg
	w.count++
	return len(p), nil
}

func TestAdapterInit(t *testing.T) {
	adapter := NewAdapter()
	if adapter == nil {
		t.Fatal("failed to create adapter")
		return
	}
	if adapter.allowMockTransfers {
		t.Fatal("mock transfers must be disabled by default")
	}
}

func TestInitResponseIsEmptyObject(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	msg := InboundMessage{
		Event:               EventInit,
		Operation:           DirectionUpload,
		ConcurrentTransfers: 2,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleInit(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleInit returned error: %v", err)
	}

	if got := buf.String(); got != "{}\n" {
		t.Fatalf("expected init response to be empty object, got %q", got)
	}
}

func TestInitRejectsInvalidOperation(t *testing.T) {
	adapter := NewAdapter()
	msg := InboundMessage{
		Event:     EventInit,
		Operation: Direction("invalid"),
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleInit(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleInit returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 400 {
		t.Fatalf("expected protocol error with code 400, got %+v", out)
	}
}

func TestAdapterRejectsBatchMaintenanceCommandsAsTransferEvents(t *testing.T) {
	for _, event := range []string{"batch-exists", "batch-delete"} {
		t.Run(event, func(t *testing.T) {
			adapter := NewAdapter()
			adapter.allowMockTransfers = true
			adapter.session = &Session{Initialized: true}

			buf := new(bytes.Buffer)
			err := adapter.handleMessage(&InboundMessage{Event: event, OID: validOID}, json.NewEncoder(buf))
			if err != nil {
				t.Fatalf("handleMessage returned error: %v", err)
			}

			var out OutboundMessage
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
				t.Fatalf("failed to decode output: %v", err)
			}
			if out.Error == nil || out.Error.Code != 400 {
				t.Fatalf("expected protocol rejection for maintenance event, got %+v", out)
			}
			if !strings.Contains(out.Error.Message, "unknown event") {
				t.Fatalf("expected unknown event rejection, got %+v", out.Error)
			}
		})
	}
}

func TestUploadFailsClosedWithoutMockMode(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, "")
	adapter.session = &Session{Initialized: true}

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "payload.bin")
	payload := []byte("payload")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}
	oid := sha256.Sum256(payload)

	msg := InboundMessage{
		Event: EventUpload,
		OID:   hex.EncodeToString(oid[:]),
		Size:  7,
		Path:  uploadPath,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 501 {
		t.Fatalf("expected not-implemented error, got %+v", out)
	}
}

func TestUploadSucceedsInMockMode(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true
	adapter.session = &Session{Initialized: true}

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "payload.bin")
	if err := os.WriteFile(uploadPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	msg := InboundMessage{
		Event: EventUpload,
		OID:   validOID,
		Size:  7,
		Path:  uploadPath,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}

	out := decodeAllMessages(t, buf.Bytes())
	if len(out) != 2 {
		t.Fatalf("expected 2 output messages, got %d", len(out))
	}
	if out[0].Event != EventProgress {
		t.Fatalf("expected progress event first, got %+v", out[0])
	}
	if out[1].Event != EventComplete || out[1].Error != nil {
		t.Fatalf("expected successful completion, got %+v", out[1])
	}
}

func TestUploadRejectsInvalidOID(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true
	adapter.session = &Session{Initialized: true}

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "payload.bin")
	if err := os.WriteFile(uploadPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	msg := InboundMessage{
		Event: EventUpload,
		OID:   "short-oid",
		Size:  7,
		Path:  uploadPath,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 400 {
		t.Fatalf("expected validation error, got %+v", out)
	}
}

func TestUploadRejectsSizeMismatch(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true
	adapter.session = &Session{Initialized: true}

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "payload.bin")
	if err := os.WriteFile(uploadPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	msg := InboundMessage{
		Event: EventUpload,
		OID:   validOID,
		Size:  999, // wrong size
		Path:  uploadPath,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 409 {
		t.Fatalf("expected size mismatch conflict, got %+v", out)
	}
}

func TestUploadPersistsObjectToLocalStore(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())
	adapter.session = &Session{Initialized: true}

	payload := []byte("real-upload-payload")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "payload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	msg := InboundMessage{
		Event: EventUpload,
		OID:   oid,
		Size:  int64(len(payload)),
		Path:  uploadPath,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}

	out := decodeAllMessages(t, buf.Bytes())
	if len(out) != 2 || out[1].Error != nil {
		t.Fatalf("expected progress + successful completion, got %+v", out)
	}

	storedPath := adapter.localObjectPath(oid)
	storedBytes, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("expected stored object file: %v", err)
	}
	if !bytes.Equal(storedBytes, payload) {
		t.Fatal("stored object bytes mismatch")
	}
}

func TestDownloadFromLocalStore(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())
	adapter.session = &Session{Initialized: true}

	payload := []byte("download-roundtrip")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	objectPath := adapter.localObjectPath(oid)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to prepare object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, payload, 0o600); err != nil {
		t.Fatalf("failed to seed object: %v", err)
	}

	msg := InboundMessage{
		Event: EventDownload,
		OID:   oid,
		Size:  int64(len(payload)),
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}

	out := decodeAllMessages(t, buf.Bytes())
	if len(out) != 2 || out[1].Error != nil {
		t.Fatalf("expected progress + successful completion, got %+v", out)
	}
	if out[1].Path == "" {
		t.Fatal("expected completion path")
	}

	downloadedBytes, err := os.ReadFile(out[1].Path)
	if err != nil {
		t.Fatalf("expected downloaded file: %v", err)
	}
	if !bytes.Equal(downloadedBytes, payload) {
		t.Fatal("downloaded object bytes mismatch")
	}
	_ = os.Remove(out[1].Path)
}

func TestDownloadRejectsCorruptStoredObject(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())
	adapter.session = &Session{Initialized: true}

	objectPath := adapter.localObjectPath(validOID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to prepare object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte("wrong"), 0o600); err != nil {
		t.Fatalf("failed to seed corrupt object: %v", err)
	}

	msg := InboundMessage{
		Event: EventDownload,
		OID:   validOID,
		Size:  5,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 500 {
		t.Fatalf("expected stored-object hash mismatch error, got %+v", out)
	}
}

func TestDownloadFailsClosedWithoutMockMode(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, "")
	adapter.session = &Session{Initialized: true}

	msg := InboundMessage{
		Event: EventDownload,
		OID:   validOID,
		Size:  32,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 501 {
		t.Fatalf("expected not-implemented error, got %+v", out)
	}
}

func TestDownloadSucceedsInMockMode(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true
	adapter.session = &Session{Initialized: true}

	msg := InboundMessage{
		Event: EventDownload,
		OID:   validOID,
		Size:  16,
	}

	buf := new(bytes.Buffer)
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}

	out := decodeAllMessages(t, buf.Bytes())
	if len(out) != 2 {
		t.Fatalf("expected 2 output messages, got %d", len(out))
	}

	complete := out[1]
	if complete.Event != EventComplete || complete.Error != nil {
		t.Fatalf("expected successful completion, got %+v", complete)
	}
	if complete.Path == "" {
		t.Fatal("expected download path in completion event")
	}

	info, err := os.Stat(complete.Path)
	if err != nil {
		t.Fatalf("expected downloaded temp file: %v", err)
	}
	if info.Size() != 16 {
		t.Fatalf("expected temp file size 16, got %d", info.Size())
	}
	_ = os.Remove(complete.Path)
}

func TestClassifyErrorAuthChallengeStates(t *testing.T) {
	cases := []struct {
		name           string
		code           int
		message        string
		wantErrorCode  string
		wantDetailPart string
	}{
		{
			name:           "totp",
			code:           401,
			message:        "two-factor authentication required",
			wantErrorCode:  "two_factor_required",
			wantDetailPart: "two-factor",
		},
		{
			name:           "data password",
			code:           401,
			message:        "mailbox/data password required for this Proton account",
			wantErrorCode:  "data_password_required",
			wantDetailPart: "mailbox/data password",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, errorCode, detail := classifyError(tc.code, tc.message)
			if state != config.StateAuthRequired {
				t.Fatalf("expected auth-required state, got %q", state)
			}
			if errorCode != tc.wantErrorCode {
				t.Fatalf("expected errorCode %q, got %q", tc.wantErrorCode, errorCode)
			}
			if !strings.Contains(strings.ToLower(detail), tc.wantDetailPart) {
				t.Fatalf("expected detail to mention %q, got %q", tc.wantDetailPart, detail)
			}
		})
	}
}

func TestSendBackendTransferErrorWritesRetryMetadata(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "status.json")
	t.Setenv(config.EnvStatusFile, statusPath)

	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	err := newBackendError(503, "drive service is unavailable", nil)
	if sendErr := adapter.sendBackendTransferError(json.NewEncoder(buf), validOID, err); sendErr != nil {
		t.Fatalf("sendBackendTransferError returned error: %v", sendErr)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode transfer error: %v", err)
	}
	if out.Error == nil || out.Error.Code != 503 {
		t.Fatalf("expected 503 transfer error, got %+v", out)
	}

	report, err := config.ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus failed: %v", err)
	}
	if report.ErrorCode != string(ErrCodeServerError) {
		t.Fatalf("status errorCode=%q, want %q", report.ErrorCode, ErrCodeServerError)
	}
	if !report.Retryable || !report.Temporary {
		t.Fatalf("expected retryable temporary status, got %+v", report)
	}
}

func TestSendBackendTransferErrorPreservesNonRetryableAuthMetadata(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "status.json")
	t.Setenv(config.EnvStatusFile, statusPath)

	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	err := newBackendErrorWithCode(401, "stored browser-fork key password required for this Proton session", nil, ErrCodeKeyPasswordRequired)
	if sendErr := adapter.sendBackendTransferError(json.NewEncoder(buf), validOID, err); sendErr != nil {
		t.Fatalf("sendBackendTransferError returned error: %v", sendErr)
	}

	report, err := config.ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus failed: %v", err)
	}
	if report.State != config.StateAuthRequired {
		t.Fatalf("status state=%q, want auth_required", report.State)
	}
	if report.ErrorCode != string(ErrCodeKeyPasswordRequired) {
		t.Fatalf("status errorCode=%q, want %q", report.ErrorCode, ErrCodeKeyPasswordRequired)
	}
	if report.Retryable || report.Temporary {
		t.Fatalf("auth blocker must not be retryable/temporary: %+v", report)
	}
}

func TestUploadRejectsPathTraversal(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true
	adapter.session = &Session{Initialized: true}

	cases := []struct {
		name string
		path string
	}{
		{"dot-dot segment", "/tmp/../etc/passwd"},
		{"dot-dot at start", "../secret"},
		{"dot-dot at end", "/tmp/foo/.."},
		{"backslash traversal", "/tmp/foo\\..\\bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &InboundMessage{
				Event: EventUpload,
				OID:   validOID,
				Size:  1,
				Path:  tc.path,
			}
			err := adapter.validateTransferRequest(msg, true)
			if err == nil {
				t.Fatal("expected path traversal error")
			}
			if err.Error() != "path traversal not allowed" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateFilePathAcceptsLegitPaths(t *testing.T) {
	cases := []string{
		"/tmp/git-lfs-objects/ab/cd/abcdef1234",
		"/home/user/.git/lfs/tmp/upload-1234",
		"relative/path/to/file.bin",
		"/var/folders/mr/some-deep/path",
	}
	for _, p := range cases {
		if err := validateFilePath(p); err != nil {
			t.Fatalf("path %q should be accepted, got: %v", p, err)
		}
	}
}

func TestUnknownEventHandling(t *testing.T) {
	adapter := NewAdapter()
	msg := InboundMessage{Event: "invalid-event"}

	buf := new(bytes.Buffer)
	if err := adapter.handleMessage(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleMessage returned error: %v", err)
	}

	var out OutboundMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if out.Error == nil || out.Error.Code != 400 {
		t.Fatalf("expected unknown-event error, got %+v", out)
	}
}

func TestPrintUsageContainsAllSections(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	output := buf.String()

	sections := []string{
		"NAME",
		"SYNOPSIS",
		"DESCRIPTION",
		"PROTOCOL COMPLIANCE",
		"BACKENDS",
		"CREDENTIAL PROVIDERS",
		"SECURITY",
		"FLAGS",
		"ENVIRONMENT VARIABLES",
		"EXAMPLES",
	}
	for _, section := range sections {
		if !strings.Contains(output, section) {
			t.Errorf("help output missing section %q", section)
		}
	}

	// Verify key content details
	details := []string{
		"git-lfs-proton-adapter",
		"lfs.standalonetransferagent",
		"proton-drive-cli",
		"PROTON_LFS_BACKEND",
		"--backend sdk",
	}
	for _, detail := range details {
		if !strings.Contains(output, detail) {
			t.Errorf("help output missing detail %q", detail)
		}
	}
}

func TestCleanupStaleTempFiles(t *testing.T) {
	// Create a temp file with the adapter prefix that looks stale
	staleFile, err := os.CreateTemp("", "git-lfs-proton-stale-test-*")
	if err != nil {
		t.Fatal(err)
	}
	stalePath := staleFile.Name()
	staleFile.Close()

	// Backdate the file to make it old enough to be cleaned up
	oldTime := time.Now().Add(-20 * time.Minute)
	os.Chtimes(stalePath, oldTime, oldTime)

	// Create a fresh temp file that should NOT be cleaned up (too new)
	freshFile, err := os.CreateTemp("", "git-lfs-proton-fresh-test-*")
	if err != nil {
		t.Fatal(err)
	}
	freshPath := freshFile.Name()
	freshFile.Close()
	defer os.Remove(freshPath)

	removed := cleanupStaleTempFiles(10 * time.Minute)

	if removed < 1 {
		t.Fatal("expected at least one stale file to be removed")
	}

	// Stale file should be gone
	if _, err := os.Stat(stalePath); err == nil {
		os.Remove(stalePath) // cleanup anyway
		t.Fatal("stale file should have been removed")
	}

	// Fresh file should still exist
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatal("fresh file should still exist")
	}
}

// --- Untested Pure Function Tests ---

func TestCalculateFileSHA256(t *testing.T) {
	t.Run("known content", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "test.bin")
		if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}

		hash, size, err := calculateFileSHA256(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if size != 5 {
			t.Fatalf("expected size 5, got %d", size)
		}
		expected := sha256.Sum256([]byte("hello"))
		if hash != hex.EncodeToString(expected[:]) {
			t.Fatalf("hash mismatch: got %s", hash)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.bin")
		if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}

		hash, size, err := calculateFileSHA256(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if size != 0 {
			t.Fatalf("expected size 0, got %d", size)
		}
		if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
			t.Fatalf("expected empty file hash, got %s", hash)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		_, _, err := calculateFileSHA256("/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("byte-perfect copy", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src.bin")
		dst := filepath.Join(dir, "dst.bin")
		content := []byte("copy-test-content")
		if err := os.WriteFile(src, content, 0o600); err != nil {
			t.Fatal(err)
		}

		if err := copyFile(src, dst); err != nil {
			t.Fatalf("copyFile failed: %v", err)
		}

		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("failed to read dst: %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Fatal("copy content mismatch")
		}

		// No temp residue
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp-") {
				t.Fatalf("temp file residue: %s", e.Name())
			}
		}
	})
	t.Run("source not found", func(t *testing.T) {
		err := copyFile("/nonexistent", filepath.Join(t.TempDir(), "dst"))
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})
}

func TestCopyIntoOpenFile(t *testing.T) {
	t.Run("content matches", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src.bin")
		content := []byte("copy-into-test")
		if err := os.WriteFile(src, content, 0o600); err != nil {
			t.Fatal(err)
		}

		dst, err := os.CreateTemp(dir, "dst-*")
		if err != nil {
			t.Fatal(err)
		}
		dstPath := dst.Name()

		if err := copyIntoOpenFile(src, dst); err != nil {
			t.Fatalf("copyIntoOpenFile failed: %v", err)
		}
		_ = dst.Close()

		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read dst: %v", err)
		}
		if !bytes.Equal(got, content) {
			t.Fatal("content mismatch")
		}
	})
	t.Run("source not found", func(t *testing.T) {
		dst, err := os.CreateTemp(t.TempDir(), "dst-*")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = dst.Close() }()

		err = copyIntoOpenFile("/nonexistent", dst)
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})
}

func TestSendProgressSequenceZeroSize(t *testing.T) {
	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)

	if err := adapter.sendProgressSequence(enc, validOID, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 progress, got %d", len(msgs))
	}
	if msgs[0].Event != EventProgress {
		t.Fatalf("expected progress event, got %+v", msgs[0])
	}
	if msgs[0].BytesSoFar != 0 {
		t.Fatalf("expected bytesSoFar=0, got %d", msgs[0].BytesSoFar)
	}
}

func TestSendProgressSequenceNegativeSize(t *testing.T) {
	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)

	if err := adapter.sendProgressSequence(enc, validOID, -1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 progress, got %d", len(msgs))
	}
	if msgs[0].BytesSoFar != 0 {
		t.Fatalf("expected bytesSoFar=0, got %d", msgs[0].BytesSoFar)
	}
}

func TestSendProgressSequenceExactChunkSize(t *testing.T) {
	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)

	if err := adapter.sendProgressSequence(enc, validOID, progressChunkSize); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 progress for exact chunk size, got %d", len(msgs))
	}
	if msgs[0].BytesSoFar != progressChunkSize {
		t.Fatalf("expected bytesSoFar=%d, got %d", progressChunkSize, msgs[0].BytesSoFar)
	}
}

func TestSendProgressSequenceSmallerThanChunk(t *testing.T) {
	adapter := NewAdapter()
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)

	if err := adapter.sendProgressSequence(enc, validOID, 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 progress for small file, got %d", len(msgs))
	}
	if msgs[0].BytesSoFar != 100 {
		t.Fatalf("expected bytesSoFar=100, got %d", msgs[0].BytesSoFar)
	}
}

func TestSendProgressSequenceLargeObjectStreamingCounter(t *testing.T) {
	adapter := NewAdapter()
	const largeSize int64 = 3*1024*1024*1024 + 123
	writer := &progressCountingWriter{t: t}

	if err := adapter.sendProgressSequence(json.NewEncoder(writer), validOID, largeSize); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCount := (largeSize + progressChunkSize - 1) / progressChunkSize
	if writer.count != expectedCount {
		t.Fatalf("expected %d progress messages, got %d", expectedCount, writer.count)
	}
	if writer.last != largeSize {
		t.Fatalf("last bytesSoFar=%d, want %d", writer.last, largeSize)
	}
	if writer.latest.BytesSince != largeSize%progressChunkSize {
		t.Fatalf("last bytesSinceLast=%d, want %d", writer.latest.BytesSince, largeSize%progressChunkSize)
	}
}

func TestUploadProgressIsPostTransfer(t *testing.T) {
	payload := []byte("post-transfer-upload")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])
	uploadPath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	adapter := NewAdapter()
	adapter.session = &Session{Initialized: true}
	buf := new(bytes.Buffer)
	adapter.backend = &observingBackend{
		uploadSize: int64(len(payload)),
		onUpload: func() {
			if buf.Len() != 0 {
				t.Fatalf("progress was emitted before backend upload returned: %q", buf.String())
			}
		},
	}

	msg := InboundMessage{Event: EventUpload, OID: oid, Size: int64(len(payload)), Path: uploadPath}
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}
	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 2 || msgs[0].Event != EventProgress || msgs[1].Event != EventComplete {
		t.Fatalf("expected post-transfer progress then complete, got %+v", msgs)
	}
}

func TestUploadUsesStreamingProgressBackend(t *testing.T) {
	payload := []byte("streaming-upload")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])
	uploadPath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	adapter := NewAdapter()
	adapter.session = &Session{Initialized: true}
	buf := new(bytes.Buffer)
	backend := &streamingObservingBackend{
		uploadSize: int64(len(payload)),
		onUploadProgress: func() {
			if !strings.Contains(buf.String(), `"event":"progress"`) {
				t.Fatalf("streaming backend did not emit progress before returning: %q", buf.String())
			}
		},
	}
	adapter.backend = backend

	msg := InboundMessage{Event: EventUpload, OID: oid, Size: int64(len(payload)), Path: uploadPath}
	if err := adapter.handleUpload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleUpload returned error: %v", err)
	}
	if !backend.uploadStreamed {
		t.Fatal("expected UploadWithProgress to be used")
	}
	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 2 || msgs[0].Event != EventProgress || msgs[1].Event != EventComplete {
		t.Fatalf("expected streamed progress then complete, got %+v", msgs)
	}
}

func TestDownloadProgressIsPostTransfer(t *testing.T) {
	payload := []byte("post-transfer-download")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])
	downloadPath := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(downloadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create download file: %v", err)
	}

	adapter := NewAdapter()
	adapter.session = &Session{Initialized: true}
	buf := new(bytes.Buffer)
	adapter.backend = &observingBackend{
		downloadPath: downloadPath,
		downloadSize: int64(len(payload)),
		onDownload: func() {
			if buf.Len() != 0 {
				t.Fatalf("progress was emitted before backend download returned: %q", buf.String())
			}
		},
	}

	msg := InboundMessage{Event: EventDownload, OID: oid, Size: int64(len(payload))}
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}
	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 2 || msgs[0].Event != EventProgress || msgs[1].Event != EventComplete {
		t.Fatalf("expected post-transfer progress then complete, got %+v", msgs)
	}
}

func TestDownloadUsesStreamingProgressBackend(t *testing.T) {
	payload := []byte("streaming-download")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])
	downloadPath := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(downloadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create download file: %v", err)
	}

	adapter := NewAdapter()
	adapter.session = &Session{Initialized: true}
	buf := new(bytes.Buffer)
	backend := &streamingObservingBackend{
		downloadPath: downloadPath,
		downloadSize: int64(len(payload)),
		onDownloadProgress: func() {
			if !strings.Contains(buf.String(), `"event":"progress"`) {
				t.Fatalf("streaming backend did not emit progress before returning: %q", buf.String())
			}
		},
	}
	adapter.backend = backend

	msg := InboundMessage{Event: EventDownload, OID: oid, Size: int64(len(payload))}
	if err := adapter.handleDownload(&msg, json.NewEncoder(buf)); err != nil {
		t.Fatalf("handleDownload returned error: %v", err)
	}
	if !backend.downloadStreamed {
		t.Fatal("expected DownloadWithProgress to be used")
	}
	msgs := decodeAllMessages(t, buf.Bytes())
	if len(msgs) != 2 || msgs[0].Event != EventProgress || msgs[1].Event != EventComplete {
		t.Fatalf("expected streamed progress then complete, got %+v", msgs)
	}
}

func TestValidateTransferRequestEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		msg     InboundMessage
		session *Session
		wantErr string
	}{
		{
			name:    "null_bytes_in_path",
			msg:     InboundMessage{Event: EventUpload, OID: validOID, Size: 1, Path: "/tmp/file\x00evil"},
			session: &Session{Initialized: true},
			wantErr: "null bytes not allowed in path",
		},
		{
			name:    "negative_size",
			msg:     InboundMessage{Event: EventUpload, OID: validOID, Size: -1, Path: "/tmp/file"},
			session: &Session{Initialized: true},
			wantErr: "invalid transfer size",
		},
		{
			name:    "empty_oid",
			msg:     InboundMessage{Event: EventUpload, OID: "", Size: 1, Path: "/tmp/file"},
			session: &Session{Initialized: true},
			wantErr: "invalid oid format",
		},
		{
			name:    "long_oid",
			msg:     InboundMessage{Event: EventUpload, OID: strings.Repeat("a", 65), Size: 1, Path: "/tmp/file"},
			session: &Session{Initialized: true},
			wantErr: "invalid oid format",
		},
		{
			name:    "whitespace_path",
			msg:     InboundMessage{Event: EventUpload, OID: validOID, Size: 1, Path: "   "},
			session: &Session{Initialized: true},
			wantErr: "missing upload path",
		},
		{
			name:    "nil_session",
			msg:     InboundMessage{Event: EventUpload, OID: validOID, Size: 1, Path: "/tmp/file"},
			session: nil,
			wantErr: "session not initialized",
		},
		{
			name:    "uninitialized_session",
			msg:     InboundMessage{Event: EventUpload, OID: validOID, Size: 1, Path: "/tmp/file"},
			session: &Session{Initialized: false},
			wantErr: "session not initialized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAdapter()
			a.session = tc.session
			err := a.validateTransferRequest(&tc.msg, true)
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}
