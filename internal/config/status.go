package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Status states written by the adapter for the tray app to observe.
const (
	StateIdle         = "idle"          // No operations in progress
	StateTransferring = "transferring"  // Transfer in progress
	StateOK           = "ok"            // Last operation succeeded
	StateError        = "error"         // Last operation failed
	StateRateLimited  = "rate_limited"  // Rate-limited by Proton API
	StateAuthRequired = "auth_required" // Authentication required or expired
	StateCaptcha      = "captcha"       // CAPTCHA verification required
)

// StatusReport is the JSON structure written to the status file.
type StatusReport struct {
	State       string    `json:"state"`                 // Current operation state (idle, transferring, ok, error, rate_limited, auth_required, captcha)
	LastOID     string    `json:"lastOid,omitempty"`     // OID of last operation
	LastOp      string    `json:"lastOp,omitempty"`      // Type of last operation (upload, download)
	Error       string    `json:"error,omitempty"`       // Human-readable error message
	ErrorCode   string    `json:"errorCode,omitempty"`   // Machine-readable error code (e.g., "rate_limited", "auth_failed", "captcha_required")
	ErrorDetail string    `json:"errorDetail,omitempty"` // Additional error context or recovery suggestions
	RetryCount  int       `json:"retryCount,omitempty"`  // Number of retry attempts (for transient errors)
	Timestamp   time.Time `json:"timestamp"`             // Timestamp of this status update
}

// WriteStatus atomically writes a status report to the status file.
// Errors are returned but should generally be logged and ignored by callers.
func WriteStatus(report StatusReport) error {
	if report.Timestamp.IsZero() {
		report.Timestamp = time.Now()
	}
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}

	path := StatusFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create status dir: %w", err)
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write status tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename status: %w", err)
	}
	return nil
}

// ReadStatus reads and parses the status file.
func ReadStatus() (StatusReport, error) {
	var report StatusReport
	data, err := os.ReadFile(StatusFilePath())
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("parse status: %w", err)
	}
	return report, nil
}
