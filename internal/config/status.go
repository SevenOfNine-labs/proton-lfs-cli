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
	StateTransferring = "transferring"  // Transfer in progress
	StateOK           = "ok"            // Last operation succeeded
	StateError        = "error"         // Last operation failed
	StateRateLimited  = "rate_limited"  // Rate-limited by Proton API
	StateAuthRequired = "auth_required" // Authentication required or expired
	StateCaptcha      = "captcha"       // CAPTCHA verification required
)

// StatusReport is the JSON structure written to the status file.
type StatusReport struct {
	State       string    `json:"state"`                 // Current operation state (transferring, ok, error, rate_limited, auth_required, captcha)
	LastOID     string    `json:"lastOid,omitempty"`     // OID of last operation
	LastOp      string    `json:"lastOp,omitempty"`      // Type of last operation (upload, download)
	Error       string    `json:"error,omitempty"`       // Human-readable error message
	ErrorCode   string    `json:"errorCode,omitempty"`   // Machine-readable error code (e.g., "rate_limited", "auth_failed", "captcha_required")
	ErrorDetail string    `json:"errorDetail,omitempty"` // Additional error context or recovery suggestions
	Retryable   bool      `json:"retryable,omitempty"`   // Whether retry may succeed without user action
	Temporary   bool      `json:"temporary,omitempty"`   // Whether the failure is expected to be transient
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

	path, err := StatusFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create status dir: %w", err)
	}
	if err := WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

// ReadStatus reads and parses the status file.
func ReadStatus() (StatusReport, error) {
	var report StatusReport
	path, err := StatusFilePath()
	if err != nil {
		return report, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("parse status: %w", err)
	}
	return report, nil
}
