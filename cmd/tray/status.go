package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"proton-lfs-cli/internal/config"
)

const pollInterval = 5 * time.Second
const refreshInterval = 15 * time.Minute

var (
	stopCh      chan struct{}
	stopOnce    sync.Once
	lastRefresh time.Time
)

func startStatusWatcher() {
	stopCh = make(chan struct{})
	go watchLoop()
}

func stopStatusWatcher() {
	stopOnce.Do(func() { close(stopCh) })
}

func watchLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial read
	applyStatus()
	applyLoginStatus()
	applyLFSStatus()

	for {
		select {
		case <-ticker.C:
			applyStatus()
			applyLoginStatus()
			applyLFSStatus()
			maybeRefreshSession()
		case <-stopCh:
			return
		}
	}
}

// sessionFilePath returns the path to the proton-drive-cli session file.
func sessionFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".proton-drive-cli", "session.json")
}

// applyLoginStatus checks whether a session file exists and updates the
// Connect menu item checkmark.
func applyLoginStatus() {
	applyConnectStatus(isSessionActive())
}

// applyLFSStatus checks whether the Proton LFS adapter is registered in
// git global config and updates the Register menu item checkmark.
func applyLFSStatus() {
	applyRegisterStatus(isLFSEnabled())
}

// maybeRefreshSession proactively refreshes the Proton session token
// every 15 minutes to keep the session alive. This calls POST /auth/v4/refresh
// (NOT a login attempt) — it never triggers CAPTCHA or rate-limiting.
func maybeRefreshSession() {
	if time.Since(lastRefresh) < refreshInterval {
		return
	}

	// Check if a session file exists (no point refreshing if not logged in)
	sf := sessionFilePath()
	if sf == "" {
		return
	}
	if _, err := os.Stat(sf); os.IsNotExist(err) {
		return
	}

	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		return
	}

	lastRefresh = time.Now()

	// Spawn in background — don't block the status poll loop
	go func() {
		cmd := exec.Command(driveCLI, "session", "refresh")
		_ = cmd.Run()
	}()
}

func applyStatus() {
	report, err := config.ReadStatus()
	if err != nil {
		systray.SetIcon(iconIdle)
		systray.SetTemplateIcon(iconIdle, iconIdle)
		systray.SetTooltip("Proton LFS")
		return
	}

	// Set icon based on state
	switch report.State {
	case config.StateIdle, config.StateOK:
		systray.SetIcon(iconOK)
		systray.SetTemplateIcon(iconOK, iconOK)
	case config.StateError:
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	case config.StateTransferring:
		systray.SetIcon(iconSyncing)
		systray.SetTemplateIcon(iconSyncing, iconSyncing)
	case config.StateRateLimited:
		// Orange/warning icon for rate-limiting (use error icon as fallback)
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	case config.StateAuthRequired:
		// Yellow/alert icon for auth required (use error icon as fallback)
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	case config.StateCaptcha:
		// Alert icon for CAPTCHA required (use error icon as fallback)
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	default:
		systray.SetIcon(iconIdle)
		systray.SetTemplateIcon(iconIdle, iconIdle)
	}

	systray.SetTooltip(statusTooltip(report))
}

func transferStatusText(report config.StatusReport) string {
	switch report.State {
	case config.StateTransferring:
		op := strings.TrimSpace(report.LastOp)
		if op == "" {
			op = "transfer"
		}
		return fmt.Sprintf("%s in progress", op)
	case config.StateError, config.StateRateLimited, config.StateAuthRequired, config.StateCaptcha:
		op := strings.TrimSpace(report.LastOp)
		if op == "" {
			op = "transfer"
		}
		return fmt.Sprintf("%s %s (%s)", op, relativeStatusTime(report.Timestamp), statusFailureSummary(report))
	case config.StateOK:
		return fmt.Sprintf("%s %s (ok)", report.LastOp, relativeStatusTime(report.Timestamp))
	default:
		return "idle"
	}
}

func statusTooltip(report config.StatusReport) string {
	switch {
	case report.State == config.StateTransferring && report.LastOp == "upload":
		return "Proton LFS — Uploading…"
	case report.State == config.StateTransferring && report.LastOp == "download":
		return "Proton LFS — Downloading…"
	case report.State == config.StateTransferring:
		return "Proton LFS — Transferring…"
	case report.State == config.StateRateLimited:
		if report.ErrorDetail != "" {
			return fmt.Sprintf("Proton LFS — Rate Limited: %s%s", truncate(report.ErrorDetail, 60), statusMetadataSuffix(report))
		}
		return "Proton LFS — Rate Limit Active" + statusMetadataSuffix(report)
	case report.State == config.StateAuthRequired:
		if report.ErrorDetail != "" {
			return fmt.Sprintf("Proton LFS — Authentication Required: %s", truncate(report.ErrorDetail, 60))
		}
		return "Proton LFS — Authentication Required"
	case report.State == config.StateCaptcha:
		return "Proton LFS — CAPTCHA Verification Required"
	case report.State == config.StateError:
		if report.ErrorCode != "" && report.ErrorDetail != "" {
			return fmt.Sprintf("Proton LFS — %s: %s%s", report.ErrorCode, truncate(report.ErrorDetail, 50), statusMetadataSuffix(report))
		}
		if report.Error != "" {
			return fmt.Sprintf("Proton LFS — Error: %s%s", truncate(report.Error, 60), statusMetadataSuffix(report))
		}
		return "Proton LFS — Error" + statusMetadataSuffix(report)
	case report.State == config.StateOK && !report.Timestamp.IsZero():
		return fmt.Sprintf("Proton LFS — Last %s %s", report.LastOp, relativeTime(report.Timestamp))
	default:
		return "Proton LFS"
	}
}

func statusFailureSummary(report config.StatusReport) string {
	parts := []string{statusFailureMessage(report)}
	if report.ErrorCode != "" {
		parts = append(parts, "code="+report.ErrorCode)
	}
	if report.Retryable {
		parts = append(parts, "retryable")
	}
	if report.Temporary {
		parts = append(parts, "temporary")
	}
	return strings.Join(parts, "; ")
}

func statusFailureMessage(report config.StatusReport) string {
	if report.Error != "" {
		return report.Error
	}
	if report.ErrorDetail != "" {
		return report.ErrorDetail
	}
	switch report.State {
	case config.StateRateLimited:
		return "rate limit active"
	case config.StateAuthRequired:
		return "authentication required"
	case config.StateCaptcha:
		return "captcha required"
	default:
		return "failed"
	}
}

func statusMetadataSuffix(report config.StatusReport) string {
	var parts []string
	if report.Retryable {
		parts = append(parts, "retryable")
	}
	if report.Temporary {
		parts = append(parts, "temporary")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func relativeStatusTime(t time.Time) string {
	if t.IsZero() {
		return "recently"
	}
	return relativeTime(t)
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-1] + "…"
}
