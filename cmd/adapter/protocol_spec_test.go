package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSpecSequenceUploadDownloadTerminate asserts the full 3-stage protocol lifecycle.
// Spec lines 115-285: Stage 1 init (lines 117-153), Stage 2 upload+download
// (lines 154-254), Stage 3 terminate (lines 275-285).
func TestRunSpecSequenceUploadDownloadTerminate(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q,"action":null}`, validOID, uploadPath),
		fmt.Sprintf(`{"event":"download","oid":"%s","size":3,"action":null}`, validOID),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 5 {
		t.Fatalf("expected 5 responses (init + 2 upload + 2 download), got %d", len(msgs))
	}

	if msgs[0].Event != "" || msgs[0].Error != nil {
		t.Fatalf("expected empty init ack message, got %+v", msgs[0])
	}
	if msgs[1].Event != EventProgress || msgs[2].Event != EventComplete {
		t.Fatalf("unexpected upload message sequence: %+v %+v", msgs[1], msgs[2])
	}
	if msgs[3].Event != EventProgress || msgs[4].Event != EventComplete {
		t.Fatalf("unexpected download message sequence: %+v %+v", msgs[3], msgs[4])
	}
	if msgs[4].Path == "" {
		t.Fatal("download completion path must be set")
	}
	if _, err := os.Stat(msgs[4].Path); err != nil {
		t.Fatalf("download completion path does not exist: %v", err)
	}
	_ = os.Remove(msgs[4].Path)
}

// TestRunPerTransferErrorDoesNotTerminateProcess asserts per-transfer errors don't kill the process.
// Spec lines 249-250: "Errors for a single transfer request should not terminate
// the process. The error should be returned in the response structure instead."
func TestRunPerTransferErrorDoesNotTerminateProcess(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	validUploadPath := filepath.Join(tmpDir, "valid.bin")
	if err := os.WriteFile(validUploadPath, []byte("value"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":10,"path":"/does/not/exist"}`, validOID),
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":5,"path":%q}`, validOID, validUploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 4 {
		t.Fatalf("expected 4 responses, got %d", len(msgs))
	}

	if msgs[1].Event != EventComplete || msgs[1].Error == nil {
		t.Fatalf("expected transfer-specific error completion, got %+v", msgs[1])
	}
	if msgs[2].Event != EventProgress || msgs[3].Event != EventComplete || msgs[3].Error != nil {
		t.Fatalf("expected second transfer to succeed, got %+v %+v", msgs[2], msgs[3])
	}
}

// TestRunInvalidInitReturnsProtocolErrorAndContinues asserts invalid init operation returns error.
// Spec lines 130, 148-152: operation must be "upload" or "download";
// init error is { "error": { "code": ..., "message": "..." } }.
func TestRunInvalidInitReturnsProtocolErrorAndContinues(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"invalid","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q}`, validOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 responses (protocol error + transfer error), got %d", len(msgs))
	}

	if msgs[0].Error == nil || msgs[0].Error.Code != 400 {
		t.Fatalf("expected init protocol error, got %+v", msgs[0])
	}
	if msgs[1].Event != EventComplete || msgs[1].Error == nil {
		t.Fatalf("expected transfer completion error after failed init, got %+v", msgs[1])
	}
}

// TestRunUploadProgressOrderingAndByteSemantics asserts upload progress ordering and byte counts.
// Spec lines 256-273: progress messages include bytesSoFar and bytesSinceLast;
// "the last one sent has bytesSoFar equal to the file size on success" (line 272-273).
func TestRunUploadProgressOrderingAndByteSemantics(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	tmpDir := t.TempDir()
	firstPayload := []byte("upload-one")
	secondPayload := []byte("upload-two-payload")

	firstPath := filepath.Join(tmpDir, "one.bin")
	if err := os.WriteFile(firstPath, firstPayload, 0o600); err != nil {
		t.Fatalf("failed to create first upload file: %v", err)
	}
	secondPath := filepath.Join(tmpDir, "two.bin")
	if err := os.WriteFile(secondPath, secondPayload, 0o600); err != nil {
		t.Fatalf("failed to create second upload file: %v", err)
	}

	firstOID := sha256.Sum256(firstPayload)
	secondOID := sha256.Sum256(secondPayload)
	firstOIDHex := hex.EncodeToString(firstOID[:])
	secondOIDHex := strings.ToUpper(hex.EncodeToString(secondOID[:]))

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, firstOIDHex, len(firstPayload), firstPath),
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, secondOIDHex, len(secondPayload), secondPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 5 {
		t.Fatalf("expected 5 responses (init + 2x progress/complete), got %d", len(msgs))
	}

	if msgs[0].Event != "" || msgs[0].Error != nil {
		t.Fatalf("expected empty init ack, got %+v", msgs[0])
	}

	if msgs[1].Event != EventProgress || msgs[2].Event != EventComplete {
		t.Fatalf("first upload must emit progress then complete, got %+v %+v", msgs[1], msgs[2])
	}
	if msgs[1].OID != firstOIDHex || msgs[2].OID != firstOIDHex {
		t.Fatalf("first upload oid mismatch between progress/complete: %+v %+v", msgs[1], msgs[2])
	}
	if msgs[1].BytesSoFar != int64(len(firstPayload)) || msgs[1].BytesSince != int64(len(firstPayload)) {
		t.Fatalf("first upload progress bytes mismatch, got %+v", msgs[1])
	}
	if msgs[2].Error != nil {
		t.Fatalf("expected first upload completion without error, got %+v", msgs[2])
	}

	secondNormalizedOID := strings.ToLower(secondOIDHex)
	if msgs[3].Event != EventProgress || msgs[4].Event != EventComplete {
		t.Fatalf("second upload must emit progress then complete, got %+v %+v", msgs[3], msgs[4])
	}
	if msgs[3].OID != secondNormalizedOID || msgs[4].OID != secondNormalizedOID {
		t.Fatalf("second upload oid mismatch between progress/complete: %+v %+v", msgs[3], msgs[4])
	}
	if msgs[3].BytesSoFar != int64(len(secondPayload)) || msgs[3].BytesSince != int64(len(secondPayload)) {
		t.Fatalf("second upload progress bytes mismatch, got %+v", msgs[3])
	}
	if msgs[4].Error != nil {
		t.Fatalf("expected second upload completion without error, got %+v", msgs[4])
	}
}

// TestRunDownloadProgressOrderingAndByteSemantics asserts download progress ordering and byte counts.
// Spec lines 256-273: progress messages include bytesSoFar and bytesSinceLast;
// "the last one sent has bytesSoFar equal to the file size on success" (line 272-273).
func TestRunDownloadProgressOrderingAndByteSemantics(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("download-progress-bytes")
	oid := sha256.Sum256(payload)
	oidHex := strings.ToUpper(hex.EncodeToString(oid[:]))

	objectPath := adapter.localObjectPath(strings.ToLower(oidHex))
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to create object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, payload, 0o600); err != nil {
		t.Fatalf("failed to seed object: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"download","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"download","oid":"%s","size":%d,"action":null}`, oidHex, len(payload)),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 3 {
		t.Fatalf("expected 3 responses (init + progress + complete), got %d", len(msgs))
	}
	if msgs[1].Event != EventProgress || msgs[2].Event != EventComplete {
		t.Fatalf("download must emit progress then complete, got %+v %+v", msgs[1], msgs[2])
	}

	normalizedOID := strings.ToLower(oidHex)
	if msgs[1].OID != normalizedOID || msgs[2].OID != normalizedOID {
		t.Fatalf("download oid mismatch between progress/complete: %+v %+v", msgs[1], msgs[2])
	}
	if msgs[1].BytesSoFar != int64(len(payload)) || msgs[1].BytesSince != int64(len(payload)) {
		t.Fatalf("download progress bytes mismatch, got %+v", msgs[1])
	}
	if msgs[2].Error != nil {
		t.Fatalf("expected download completion without error, got %+v", msgs[2])
	}
	if msgs[2].Path == "" {
		t.Fatal("expected download completion path")
	}
	if _, err := os.Stat(msgs[2].Path); err != nil {
		t.Fatalf("expected download completion path to exist: %v", err)
	}
	_ = os.Remove(msgs[2].Path)
}

type failAfterNWriter struct {
	successfulWrites int
	failAt           int
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	if w.successfulWrites >= w.failAt {
		return 0, io.ErrClosedPipe
	}
	w.successfulWrites++
	return len(p), nil
}

// TestRunReturnsErrorOnPartialWriteFailure asserts Run() fails when stdout write breaks mid-protocol.
// Spec lines 108-111: the transfer process must write JSON to stdout with a line feed;
// if the output pipe breaks, the process should propagate the write error.
func TestRunReturnsErrorOnPartialWriteFailure(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q}`, validOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	writer := &failAfterNWriter{failAt: 1}
	err := adapter.Run(strings.NewReader(input), writer)
	if err == nil {
		t.Fatal("expected run to fail when output writer fails after init ack")
	}
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected closed pipe error, got: %v", err)
	}
}

// TestRunUploadMultiChunkProgressMonotonic asserts multi-chunk upload progress is monotonically increasing.
// Spec lines 256-273: bytesSoFar must increase with each progress message;
// bytesSinceLast must equal the delta between consecutive bytesSoFar values.
func TestRunUploadMultiChunkProgressMonotonic(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	size := int(progressChunkSize*2 + 17)
	payload := bytes.Repeat([]byte("a"), size)
	oid := sha256.Sum256(payload)
	oidHex := hex.EncodeToString(oid[:])

	uploadPath := filepath.Join(t.TempDir(), "chunked-upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, oidHex, size, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	expectedProgressCount := int((int64(size) + progressChunkSize - 1) / progressChunkSize)
	expectedTotal := 1 + expectedProgressCount + 1 // init ack + progress + complete
	if len(msgs) != expectedTotal {
		t.Fatalf("expected %d responses, got %d", expectedTotal, len(msgs))
	}

	lastBytes := int64(0)
	for i := 1; i <= expectedProgressCount; i++ {
		msg := msgs[i]
		if msg.Event != EventProgress {
			t.Fatalf("expected progress at index %d, got %+v", i, msg)
		}
		if msg.OID != oidHex {
			t.Fatalf("expected progress oid %s, got %s", oidHex, msg.OID)
		}
		if msg.BytesSoFar <= lastBytes {
			t.Fatalf("bytesSoFar must be monotonic, prev=%d current=%d", lastBytes, msg.BytesSoFar)
		}
		expectedDelta := msg.BytesSoFar - lastBytes
		if msg.BytesSince != expectedDelta {
			t.Fatalf("bytesSinceLast mismatch at index %d: got %d expected %d", i, msg.BytesSince, expectedDelta)
		}
		lastBytes = msg.BytesSoFar
	}
	if lastBytes != int64(size) {
		t.Fatalf("expected final bytesSoFar=%d, got %d", size, lastBytes)
	}

	complete := msgs[len(msgs)-1]
	if complete.Event != EventComplete || complete.Error != nil || complete.OID != oidHex {
		t.Fatalf("unexpected completion message: %+v", complete)
	}
}

// TestRunDownloadMultiChunkProgressMonotonic asserts multi-chunk download progress is monotonically increasing.
// Spec lines 256-273: bytesSoFar must increase with each progress message;
// bytesSinceLast must equal the delta between consecutive bytesSoFar values.
func TestRunDownloadMultiChunkProgressMonotonic(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	size := int(progressChunkSize*3 + 11)
	payload := bytes.Repeat([]byte("b"), size)
	oid := sha256.Sum256(payload)
	oidHex := hex.EncodeToString(oid[:])

	objectPath := adapter.localObjectPath(oidHex)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to create object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, payload, 0o600); err != nil {
		t.Fatalf("failed to seed object: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"download","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"download","oid":"%s","size":%d,"action":null}`, oidHex, size),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	expectedProgressCount := int((int64(size) + progressChunkSize - 1) / progressChunkSize)
	expectedTotal := 1 + expectedProgressCount + 1 // init ack + progress + complete
	if len(msgs) != expectedTotal {
		t.Fatalf("expected %d responses, got %d", expectedTotal, len(msgs))
	}

	lastBytes := int64(0)
	for i := 1; i <= expectedProgressCount; i++ {
		msg := msgs[i]
		if msg.Event != EventProgress {
			t.Fatalf("expected progress at index %d, got %+v", i, msg)
		}
		if msg.OID != oidHex {
			t.Fatalf("expected progress oid %s, got %s", oidHex, msg.OID)
		}
		if msg.BytesSoFar <= lastBytes {
			t.Fatalf("bytesSoFar must be monotonic, prev=%d current=%d", lastBytes, msg.BytesSoFar)
		}
		expectedDelta := msg.BytesSoFar - lastBytes
		if msg.BytesSince != expectedDelta {
			t.Fatalf("bytesSinceLast mismatch at index %d: got %d expected %d", i, msg.BytesSince, expectedDelta)
		}
		lastBytes = msg.BytesSoFar
	}
	if lastBytes != int64(size) {
		t.Fatalf("expected final bytesSoFar=%d, got %d", size, lastBytes)
	}

	complete := msgs[len(msgs)-1]
	if complete.Event != EventComplete || complete.Error != nil || complete.OID != oidHex {
		t.Fatalf("unexpected completion message: %+v", complete)
	}
	if complete.Path == "" {
		t.Fatal("expected completion path for download")
	}
	_ = os.Remove(complete.Path)
}

// --- Protocol Compliance Tests ---
// Spec: submodules/git-lfs/docs/custom-transfers.md

// TestSpecUploadCompleteOmitsPath asserts upload complete has no "path" field.
// Spec lines 185-190: upload complete is { "event": "complete", "oid": "..." }
// with no path field (path is only required for downloads).
func TestSpecUploadCompleteOmitsPath(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("upload-path-check")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, oid, len(payload), uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	for _, line := range lines {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if ev, ok := raw["event"]; ok && string(ev) == `"complete"` {
			if _, hasPath := raw["path"]; hasPath {
				t.Fatal("upload complete message must not include 'path' field")
			}
		}
	}
}

// TestSpecDownloadCompleteMustIncludePath asserts download complete includes a non-empty "path".
// Spec lines 229-237: download complete is { "event": "complete", "oid": "...", "path": "/path/to/file" }
// where path is a file containing the downloaded data.
func TestSpecDownloadCompleteMustIncludePath(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("download-path-check")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	objectPath := adapter.localObjectPath(oid)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to create object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, payload, 0o600); err != nil {
		t.Fatalf("failed to seed object: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"download","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"download","oid":"%s","size":%d,"action":null}`, oid, len(payload)),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	foundComplete := false
	for _, line := range lines {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if ev, ok := raw["event"]; ok && string(ev) == `"complete"` {
			foundComplete = true
			pathRaw, hasPath := raw["path"]
			if !hasPath {
				t.Fatal("download complete message must include 'path' field")
			}
			var pathStr string
			if err := json.Unmarshal(pathRaw, &pathStr); err != nil {
				t.Fatalf("failed to parse path: %v", err)
			}
			if pathStr == "" {
				t.Fatal("download complete path must not be empty")
			}
			_ = os.Remove(pathStr)
		}
	}
	if !foundComplete {
		t.Fatal("no complete message found in output")
	}
}

// TestSpecTerminateProducesNoOutput asserts terminate produces no response.
// Spec lines 280-285: "On receiving this message the transfer process should
// clean up and terminate. No response is expected."
func TestSpecTerminateProducesNoOutput(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response (init ack only), got %d", len(msgs))
	}
	if msgs[0].Event != "" || msgs[0].Error != nil {
		t.Fatalf("expected empty init ack, got %+v", msgs[0])
	}
}

// TestSpecTerminateExitClean asserts Run() returns nil (exit code 0) after terminate.
// Spec lines 284, 291: "the exit code should be 0 even if some transfers failed."
func TestSpecTerminateExitClean(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	err := adapter.Run(strings.NewReader(input), out)
	if err != nil {
		t.Fatalf("Run should return nil after terminate, got: %v", err)
	}
}

// TestSpecLineDelimitedJSON asserts every output line is valid single-line JSON.
// Spec lines 100-111: "each JSON structure will be sent and received on a
// single line" as per Line Delimited JSON.
func TestSpecLineDelimitedJSON(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q}`, validOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			t.Fatalf("output line %d is not valid JSON: %q", i+1, string(line))
		}
	}
}

// TestSpecActionNullIsAccepted asserts upload with "action":null succeeds.
// Spec lines 179-180: "action is null for standalone transfer agents."
func TestSpecActionNullIsAccepted(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q,"action":null}`, validOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 3 {
		t.Fatalf("expected 3 responses (init + progress + complete), got %d", len(msgs))
	}
	if msgs[2].Event != EventComplete || msgs[2].Error != nil {
		t.Fatalf("expected successful completion with action:null, got %+v", msgs[2])
	}
}

// TestSpecActionWithDataIsIgnored asserts upload succeeds when action has data.
// Spec lines 174-180: action contains href/header from batch API; standalone
// agents receive null but should tolerate non-null values.
func TestSpecActionWithDataIsIgnored(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":4,"path":%q,"action":{"href":"https://example.com","header":{"Authorization":"Bearer test"}}}`, validOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 3 {
		t.Fatalf("expected 3 responses (init + progress + complete), got %d", len(msgs))
	}
	if msgs[2].Event != EventComplete || msgs[2].Error != nil {
		t.Fatalf("expected successful completion with action data, got %+v", msgs[2])
	}
}

// TestSpecOIDNormalizationInResponses asserts uppercase OID is normalized to lowercase.
// Spec lines 186, 190: oid in responses must match the object identifier;
// adapter normalizes to lowercase for consistency.
func TestSpecOIDNormalizationInResponses(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("normalize-test")
	oidBytes := sha256.Sum256(payload)
	upperOID := strings.ToUpper(hex.EncodeToString(oidBytes[:]))
	lowerOID := strings.ToLower(upperOID)

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, upperOID, len(payload), uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	for _, msg := range msgs {
		if msg.OID != "" && msg.OID != lowerOID {
			t.Fatalf("expected normalized OID %s, got %s", lowerOID, msg.OID)
		}
	}
}

// TestSpecProgressFinalBytesSoFarEqualsFileSize asserts the last progress bytesSoFar == file size.
// Spec lines 272-273: "the last one sent has bytesSoFar equal to the file size on success."
func TestSpecProgressFinalBytesSoFarEqualsFileSize(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("progress-final-check")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	uploadPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(uploadPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, oid, len(payload), uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	var lastProgress *OutboundMessage
	for i := range msgs {
		if msgs[i].Event == EventProgress {
			lastProgress = &msgs[i]
		}
	}
	if lastProgress == nil {
		t.Fatal("expected at least one progress message")
	}
	if lastProgress.BytesSoFar != int64(len(payload)) {
		t.Fatalf("last progress bytesSoFar=%d, expected %d", lastProgress.BytesSoFar, len(payload))
	}
}

// TestSpecZeroByteFileUpload asserts zero-byte upload succeeds.
// Spec lines 182-190: upload protocol must handle zero-size objects;
// progress + complete sequence still expected.
func TestSpecZeroByteFileUpload(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	emptyOID := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	uploadPath := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(uploadPath, []byte{}, 0o600); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":0,"path":%q}`, emptyOID, uploadPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	complete := msgs[len(msgs)-1]
	if complete.Event != EventComplete || complete.Error != nil {
		t.Fatalf("expected successful zero-byte upload, got %+v", complete)
	}
}

// TestSpecZeroByteFileDownload asserts zero-byte download succeeds with valid path.
// Spec lines 222-237: download complete must include path to a file containing
// the downloaded data, even for zero-byte objects.
func TestSpecZeroByteFileDownload(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	emptyOID := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	objectPath := adapter.localObjectPath(emptyOID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		t.Fatalf("failed to create object dir: %v", err)
	}
	if err := os.WriteFile(objectPath, []byte{}, 0o600); err != nil {
		t.Fatalf("failed to seed empty object: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"download","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"download","oid":"%s","size":0,"action":null}`, emptyOID),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	complete := msgs[len(msgs)-1]
	if complete.Event != EventComplete || complete.Error != nil {
		t.Fatalf("expected successful zero-byte download, got %+v", complete)
	}
	if complete.Path == "" {
		t.Fatal("download completion must include path")
	}
	info, err := os.Stat(complete.Path)
	if err != nil {
		t.Fatalf("expected download file to exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected 0-byte file, got %d bytes", info.Size())
	}
	_ = os.Remove(complete.Path)
}

// TestSpecPerTransferErrorChain asserts per-transfer errors don't terminate the process.
// Spec lines 249-250: "Errors for a single transfer request should not terminate
// the process. The error should be returned in the response structure instead."
func TestSpecPerTransferErrorChain(t *testing.T) {
	adapter := NewAdapter()
	configureLocalBackend(adapter, t.TempDir())

	payload := []byte("chain-test")
	oidBytes := sha256.Sum256(payload)
	oid := hex.EncodeToString(oidBytes[:])

	validPath := filepath.Join(t.TempDir(), "valid.bin")
	if err := os.WriteFile(validPath, payload, 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":10,"path":"/does/not/exist"}`, validOID),
		fmt.Sprintf(`{"event":"upload","oid":"%s","size":%d,"path":%q}`, oid, len(payload), validPath),
		fmt.Sprintf(`{"event":"upload","oid":"short","size":1,"path":%q}`, validPath),
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) < 5 {
		t.Fatalf("expected at least 5 responses, got %d", len(msgs))
	}

	// Transfer 1: error complete
	if msgs[1].Event != EventComplete || msgs[1].Error == nil {
		t.Fatalf("transfer 1 should be error completion, got %+v", msgs[1])
	}
	// Transfer 2: progress + success complete
	if msgs[2].Event != EventProgress {
		t.Fatalf("transfer 2 progress expected, got %+v", msgs[2])
	}
	if msgs[3].Event != EventComplete || msgs[3].Error != nil {
		t.Fatalf("transfer 2 should succeed, got %+v", msgs[3])
	}
	// Transfer 3: error complete (bad OID)
	if msgs[4].Event != EventComplete || msgs[4].Error == nil {
		t.Fatalf("transfer 3 should be error completion, got %+v", msgs[4])
	}
}

// TestSpecMalformedJSONInput asserts malformed JSON returns a protocol error without panic.
// Spec lines 289-291: "Any unexpected fatal errors in the transfer process
// should set the exit code to non-zero and print information to stderr."
func TestSpecMalformedJSONInput(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	out := new(bytes.Buffer)
	err := adapter.Run(strings.NewReader("this is not json at all\n"), out)
	if err != nil {
		t.Fatalf("Run should not return error for malformed input, got: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 protocol error, got %d", len(msgs))
	}
	if msgs[0].Error == nil {
		t.Fatal("expected error in protocol response")
	}
}

// TestSpecEmptyEventField asserts that an empty event field returns code 400.
// Spec lines 129-130, 170, 211: event must be one of "init", "upload",
// "download", or "terminate"; an empty string is not a recognized event.
func TestSpecEmptyEventField(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	input := "{\"event\":\"\"}\n"
	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}
	if msgs[0].Error == nil || msgs[0].Error.Code != 400 {
		t.Fatalf("expected error code 400, got %+v", msgs[0])
	}
}

// TestSpecTransferBeforeInit asserts upload without init returns "session not initialized".
// Spec lines 115-146: Stage 1 init must precede Stage 2 transfers;
// the transfer process must be initialized before handling upload/download events.
func TestSpecTransferBeforeInit(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	tmpDir := t.TempDir()
	uploadPath := filepath.Join(tmpDir, "upload.bin")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o600); err != nil {
		t.Fatalf("failed to create upload file: %v", err)
	}

	input := fmt.Sprintf("{\"event\":\"upload\",\"oid\":\"%s\",\"size\":4,\"path\":%q}\n", validOID, uploadPath)
	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 1 {
		t.Fatalf("expected 1 response, got %d", len(msgs))
	}
	if msgs[0].Error == nil {
		t.Fatal("expected error for transfer before init")
	}
	if !strings.Contains(msgs[0].Error.Message, "session not initialized") {
		t.Fatalf("expected 'session not initialized', got %q", msgs[0].Error.Message)
	}
}

// TestSpecTerminateWithoutInit asserts terminate with no prior init returns nil (clean).
// Spec lines 275-285: Stage 3 terminate triggers clean shutdown;
// even without prior init, the process should exit cleanly with no output.
func TestSpecTerminateWithoutInit(t *testing.T) {
	adapter := NewAdapter()

	input := "{\"event\":\"terminate\"}\n"
	out := new(bytes.Buffer)
	err := adapter.Run(strings.NewReader(input), out)
	if err != nil {
		t.Fatalf("terminate without init should return nil, got: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 0 {
		t.Fatalf("expected no output for terminate without init, got %d messages", len(msgs))
	}
}

// TestSpecDuplicateInitMessages asserts two init messages both produce {} ack.
// Spec lines 140-146: init response is an empty JSON object confirmation;
// spec does not prohibit multiple init messages, each should be acknowledged.
func TestSpecDuplicateInitMessages(t *testing.T) {
	adapter := NewAdapter()
	adapter.allowMockTransfers = true

	input := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		`{"event":"init","operation":"download","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	out := new(bytes.Buffer)
	if err := adapter.Run(strings.NewReader(input), out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	msgs := decodeAllMessages(t, out.Bytes())
	if len(msgs) != 2 {
		t.Fatalf("expected 2 init ack responses, got %d", len(msgs))
	}
	for i, msg := range msgs {
		if msg.Event != "" || msg.Error != nil {
			t.Fatalf("init ack %d should be empty object, got %+v", i+1, msg)
		}
	}
}
