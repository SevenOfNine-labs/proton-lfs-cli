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
	StateIdle         = "idle"
	StateTransferring = "transferring"
	StateOK           = "ok"
	StateError        = "error"
)

// StatusReport is the JSON structure written to the status file.
type StatusReport struct {
	State     string    `json:"state"`
	LastOID   string    `json:"lastOid,omitempty"`
	LastOp    string    `json:"lastOp,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
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
