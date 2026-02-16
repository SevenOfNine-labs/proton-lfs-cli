package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatusReportPersistence(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	originalPath := StatusFilePath()
	defer func() {
		os.Setenv("PROTON_LFS_STATUS_FILE", originalPath)
	}()

	testPath := filepath.Join(tmpDir, "status.json")
	os.Setenv("PROTON_LFS_STATUS_FILE", testPath)

	// Write status
	originalStatus := StatusReport{
		State:       StateOK,
		LastOID:     "abc123",
		LastOp:      "upload",
		Error:       "",
		ErrorCode:   "",
		ErrorDetail: "",
		RetryCount:  0,
		Timestamp:   time.Now().Truncate(time.Second), // Truncate for comparison
	}

	err := WriteStatus(originalStatus)
	if err != nil {
		t.Fatalf("WriteStatus failed: %v", err)
	}

	// Read status
	readStatus, err := ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus failed: %v", err)
	}

	// Compare
	if readStatus.State != originalStatus.State {
		t.Errorf("State mismatch: got %s, want %s", readStatus.State, originalStatus.State)
	}
	if readStatus.LastOID != originalStatus.LastOID {
		t.Errorf("LastOID mismatch: got %s, want %s", readStatus.LastOID, originalStatus.LastOID)
	}
	if readStatus.LastOp != originalStatus.LastOp {
		t.Errorf("LastOp mismatch: got %s, want %s", readStatus.LastOp, originalStatus.LastOp)
	}

	// Timestamp comparison (within 1 second tolerance)
	timeDiff := readStatus.Timestamp.Sub(originalStatus.Timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff > time.Second {
		t.Errorf("Timestamp mismatch: got %v, want %v", readStatus.Timestamp, originalStatus.Timestamp)
	}
}

func TestStatusWithErrorFields(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := StatusFilePath()
	defer func() {
		os.Setenv("PROTON_LFS_STATUS_FILE", originalPath)
	}()

	testPath := filepath.Join(tmpDir, "status.json")
	os.Setenv("PROTON_LFS_STATUS_FILE", testPath)

	// Write status with error fields
	originalStatus := StatusReport{
		State:       StateRateLimited,
		LastOID:     "def456",
		LastOp:      "download",
		Error:       "Rate limit exceeded",
		ErrorCode:   "rate_limited",
		ErrorDetail: "Wait before retrying operations",
		RetryCount:  3,
		Timestamp:   time.Now(),
	}

	err := WriteStatus(originalStatus)
	if err != nil {
		t.Fatalf("WriteStatus failed: %v", err)
	}

	// Read status
	readStatus, err := ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus failed: %v", err)
	}

	// Compare error fields
	if readStatus.ErrorCode != originalStatus.ErrorCode {
		t.Errorf("ErrorCode mismatch: got %s, want %s", readStatus.ErrorCode, originalStatus.ErrorCode)
	}
	if readStatus.ErrorDetail != originalStatus.ErrorDetail {
		t.Errorf("ErrorDetail mismatch: got %s, want %s", readStatus.ErrorDetail, originalStatus.ErrorDetail)
	}
	if readStatus.RetryCount != originalStatus.RetryCount {
		t.Errorf("RetryCount mismatch: got %d, want %d", readStatus.RetryCount, originalStatus.RetryCount)
	}
}

func TestStatusStates(t *testing.T) {
	tests := []struct {
		name  string
		state string
	}{
		{"idle state", StateIdle},
		{"transferring state", StateTransferring},
		{"ok state", StateOK},
		{"error state", StateError},
		{"rate limited state", StateRateLimited},
		{"auth required state", StateAuthRequired},
		{"captcha state", StateCaptcha},
	}

	tmpDir := t.TempDir()
	originalPath := StatusFilePath()
	defer func() {
		os.Setenv("PROTON_LFS_STATUS_FILE", originalPath)
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPath := filepath.Join(tmpDir, "status-"+tt.state+".json")
			os.Setenv("PROTON_LFS_STATUS_FILE", testPath)

			status := StatusReport{
				State:     tt.state,
				Timestamp: time.Now(),
			}

			err := WriteStatus(status)
			if err != nil {
				t.Fatalf("WriteStatus failed for %s: %v", tt.state, err)
			}

			readStatus, err := ReadStatus()
			if err != nil {
				t.Fatalf("ReadStatus failed for %s: %v", tt.state, err)
			}

			if readStatus.State != tt.state {
				t.Errorf("State mismatch for %s: got %s, want %s", tt.name, readStatus.State, tt.state)
			}
		})
	}
}

func TestReadStatusNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := StatusFilePath()
	defer func() {
		os.Setenv("PROTON_LFS_STATUS_FILE", originalPath)
	}()

	testPath := filepath.Join(tmpDir, "nonexistent.json")
	os.Setenv("PROTON_LFS_STATUS_FILE", testPath)

	_, err := ReadStatus()
	if err == nil {
		t.Error("Expected error reading non-existent status file")
	}
}

func TestStatusAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	originalPath := StatusFilePath()
	defer func() {
		os.Setenv("PROTON_LFS_STATUS_FILE", originalPath)
	}()

	testPath := filepath.Join(tmpDir, "status.json")
	os.Setenv("PROTON_LFS_STATUS_FILE", testPath)

	// Write multiple times to ensure atomic writes don't corrupt
	for i := 0; i < 10; i++ {
		status := StatusReport{
			State:     StateOK,
			LastOID:   "oid" + string(rune(i)),
			Timestamp: time.Now(),
		}

		err := WriteStatus(status)
		if err != nil {
			t.Fatalf("WriteStatus failed on iteration %d: %v", i, err)
		}

		// Verify we can read immediately after write
		_, err = ReadStatus()
		if err != nil {
			t.Fatalf("ReadStatus failed on iteration %d: %v", i, err)
		}
	}
}
