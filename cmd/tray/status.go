package main

import (
	"encoding/json"
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
const refreshBeforeExpiry = 10 * time.Minute
const refreshRetryInterval = time.Minute
const refreshFallbackInterval = 12 * time.Hour

var (
	stopCh     chan struct{}
	stopOnce   sync.Once
	refreshMu  sync.Mutex
	refreshNow = time.Now
)

var refreshState = sessionRefreshState{}

type sessionMetadata struct {
	SessionID            string   `json:"sessionId"`
	UID                  string   `json:"uid"`
	AccessToken          string   `json:"accessToken"`
	RefreshToken         string   `json:"refreshToken"`
	Scopes               []string `json:"scopes"`
	PasswordMode         int      `json:"passwordMode"`
	AuthMode             string   `json:"authMode"`
	KeyPasswordPersisted bool     `json:"keyPasswordPersisted"`
	TokenExpiresAt       int64    `json:"tokenExpiresAt"`
}

type authReadiness struct {
	statusTitle        string
	sessionTitle       string
	transferTitle      string
	refreshTitle       string
	tooltip            string
	actionTitle        string
	action             authMenuAction
	dataPasswordActive bool
	signedIn           bool
	ready              bool
	blocked            bool
}

type sessionRefreshState struct {
	InProgress  bool
	LastAttempt time.Time
	LastSuccess time.Time
	LastError   string
	NextAttempt time.Time
}

type refreshDecision struct {
	Attempt     bool
	NextAttempt time.Time
}

func timeNow() time.Time {
	return refreshNow()
}

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

// applyLoginStatus checks local auth readiness and updates user-facing labels.
func applyLoginStatus() {
	applyAuthReadiness(inspectAuthReadiness(timeNow()))
}

// applyLFSStatus checks whether the Proton LFS adapter is registered in
// git global config and updates the Register menu item checkmark.
func applyLFSStatus() {
	applyRegisterStatus(isLFSEnabled())
}

// maybeRefreshSession proactively refreshes the Proton session token near
// expiry. This calls POST /auth/v4/refresh and never attempts account login.
func maybeRefreshSession() {
	meta, ok := readSessionMetadata()
	if !ok || meta.RefreshToken == "" {
		return
	}
	now := timeNow()
	decision := decideRefresh(now, meta, snapshotRefreshState())
	if !decision.Attempt {
		setRefreshNextAttempt(decision.NextAttempt)
		return
	}
	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		recordRefreshFailure(now, "proton-drive-cli not found")
		applyLoginStatus()
		return
	}
	if !markRefreshStarted(now) {
		return
	}
	applyLoginStatus()
	go func() {
		cmd := exec.Command(driveCLI, "session", "refresh")
		out, err := cmd.CombinedOutput()
		logSubprocessOutput("refresh", out)
		finishedAt := timeNow()
		if err != nil {
			recordRefreshFailure(finishedAt, err.Error())
			trayLog.Printf("refresh: failed: %v", err)
		} else {
			recordRefreshSuccess(finishedAt)
			trayLog.Print("refresh: session token refreshed")
		}
		applyLoginStatus()
	}()
}

func readSessionMetadata() (sessionMetadata, bool) {
	sf := sessionFilePath()
	if sf == "" {
		return sessionMetadata{}, false
	}
	data, err := os.ReadFile(sf)
	if err != nil {
		return sessionMetadata{}, false
	}
	var meta sessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return sessionMetadata{}, false
	}
	return meta, true
}

func validSessionShape(meta sessionMetadata) bool {
	return meta.SessionID != "" &&
		meta.UID != "" &&
		meta.AccessToken != "" &&
		meta.RefreshToken != "" &&
		meta.Scopes != nil &&
		meta.PasswordMode > 0
}

func tokenExpiry(meta sessionMetadata) time.Time {
	if meta.TokenExpiresAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(meta.TokenExpiresAt)
}

func hasConfiguredDataCredential() bool {
	prefs := config.LoadPrefs()
	if strings.TrimSpace(prefs.DataCredentialProvider) != "" {
		return true
	}
	if config.EnvTrim(config.EnvDataCredentialProvider) != "" {
		return true
	}
	return configuredDataCredentialProviderFromGit() != ""
}

func configuredDataCredentialProviderFromGit() string {
	out, err := exec.Command("git", "config", "--global", "lfs.customtransfer.proton.args").Output()
	if err != nil {
		return ""
	}
	return parseFlagValue(string(out), "--data-credential-provider")
}

func parseFlagValue(text string, flag string) string {
	fields := strings.Fields(text)
	for i, field := range fields {
		if field == flag && i+1 < len(fields) {
			return strings.Trim(fields[i+1], `"'`)
		}
		if strings.HasPrefix(field, flag+"=") {
			return strings.Trim(strings.TrimPrefix(field, flag+"="), `"'`)
		}
	}
	return ""
}

func inspectAuthReadiness(now time.Time) authReadiness {
	meta, hasSession := readSessionMetadata()
	health := snapshotRefreshState()
	base := authReadiness{
		statusTitle:   "Status: Not connected",
		sessionTitle:  "Session: Not connected",
		transferTitle: "Transfers: Sign-in required",
		actionTitle:   "Connect to Proton...",
		action:        authActionConnect,
		refreshTitle:  refreshTitle(now, meta, hasSession, health),
		tooltip:       "Proton LFS - Not connected",
	}
	if !hasSession {
		return base
	}
	base.signedIn = true
	base.actionTitle = "Disconnect from Proton"
	base.action = authActionDisconnect
	base.sessionTitle = "Session: Signed in"

	if !validSessionShape(meta) {
		base.statusTitle = "Status: Setup needed"
		base.sessionTitle = "Session: Invalid"
		base.transferTitle = "Transfers: Reconnect required"
		base.tooltip = "Proton LFS - Setup needed: invalid saved session"
		base.blocked = true
		return base
	}
	if expiry := tokenExpiry(meta); !expiry.IsZero() && !now.Before(expiry) {
		base.statusTitle = "Status: Setup needed"
		base.sessionTitle = "Session: Expired"
		base.transferTitle = "Transfers: Refresh required"
		base.tooltip = "Proton LFS - Setup needed: session refresh required"
		base.blocked = true
		return base
	}
	if meta.PasswordMode == 2 && !hasConfiguredDataCredential() {
		base.statusTitle = "Status: Setup needed"
		base.transferTitle = "Transfers: Data password needed"
		base.dataPasswordActive = true
		base.tooltip = "Proton LFS - Setup needed: data password required"
		base.blocked = true
		return base
	}
	base.statusTitle = "Status: Ready"
	base.transferTitle = "Transfers: Ready"
	base.tooltip = "Proton LFS - Ready"
	base.ready = true
	return base
}

func applyAuthReadiness(readiness authReadiness) {
	if mStatus != nil {
		mStatus.SetTitle(readiness.statusTitle)
	}
	if mSession != nil {
		mSession.SetTitle(readiness.sessionTitle)
	}
	if mTransfers != nil {
		mTransfers.SetTitle(readiness.transferTitle)
	}
	if mRefresh != nil {
		mRefresh.SetTitle(readiness.refreshTitle)
	}
	if mPrimaryAuth != nil {
		mPrimaryAuth.SetTitle(readiness.actionTitle)
		if readiness.action == authActionNone {
			mPrimaryAuth.Disable()
		} else {
			mPrimaryAuth.Enable()
		}
	}
	if mDataPassword != nil {
		if readiness.dataPasswordActive {
			mDataPassword.Enable()
		} else {
			mDataPassword.Disable()
		}
	}
	currentAuthAction = readiness.action
	if !authReadinessShouldOverrideTransferStatus(readiness) {
		return
	}
	switch {
	case readiness.ready:
		systray.SetIcon(iconOK)
		systray.SetTemplateIcon(iconOK, iconOK)
	case readiness.blocked:
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	default:
		systray.SetIcon(iconIdle)
		systray.SetTemplateIcon(iconIdle, iconIdle)
	}
	systray.SetTooltip(readiness.tooltip)
}

func authReadinessShouldOverrideTransferStatus(readiness authReadiness) bool {
	if readiness.blocked || !readiness.signedIn {
		return true
	}
	report, err := config.ReadStatus()
	if err != nil {
		return true
	}
	switch report.State {
	case config.StateIdle, config.StateOK:
		return true
	default:
		return false
	}
}

func snapshotRefreshState() sessionRefreshState {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	return refreshState
}

func setRefreshNextAttempt(next time.Time) {
	if next.IsZero() {
		return
	}
	refreshMu.Lock()
	if refreshState.NextAttempt.IsZero() || !refreshState.NextAttempt.Equal(next) {
		refreshState.NextAttempt = next
	}
	refreshMu.Unlock()
}

func markRefreshStarted(now time.Time) bool {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if refreshState.InProgress {
		return false
	}
	refreshState.InProgress = true
	refreshState.LastAttempt = now
	refreshState.LastError = ""
	return true
}

func recordRefreshSuccess(now time.Time) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastSuccess = now
	refreshState.LastError = ""
	if meta, ok := readSessionMetadata(); ok {
		refreshState.NextAttempt = nextRefreshAttempt(now, meta)
	} else {
		refreshState.NextAttempt = time.Time{}
	}
}

func recordRefreshFailure(now time.Time, message string) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastAttempt = now
	refreshState.LastError = truncate(strings.TrimSpace(message), 120)
	refreshState.NextAttempt = now.Add(refreshRetryInterval)
}

func decideRefresh(now time.Time, meta sessionMetadata, health sessionRefreshState) refreshDecision {
	if health.InProgress {
		return refreshDecision{}
	}
	if !health.NextAttempt.IsZero() && now.Before(health.NextAttempt) {
		return refreshDecision{NextAttempt: health.NextAttempt}
	}
	next := nextRefreshAttempt(now, meta)
	if next.IsZero() || !now.Before(next) {
		return refreshDecision{Attempt: true}
	}
	return refreshDecision{NextAttempt: next}
}

func nextRefreshAttempt(now time.Time, meta sessionMetadata) time.Time {
	expiry := tokenExpiry(meta)
	if expiry.IsZero() {
		return now.Add(refreshFallbackInterval)
	}
	return expiry.Add(-refreshBeforeExpiry)
}

func refreshTitle(now time.Time, meta sessionMetadata, hasSession bool, health sessionRefreshState) string {
	switch {
	case !hasSession:
		return "Refresh: No session"
	case health.InProgress:
		return "Refresh: Updating..."
	case health.LastError != "":
		if !health.NextAttempt.IsZero() && health.NextAttempt.After(now) {
			return "Refresh: Failed; retry in " + compactDuration(health.NextAttempt.Sub(now))
		}
		return "Refresh: Failed"
	case !health.LastSuccess.IsZero():
		return "Refresh: Last refreshed " + relativeTime(health.LastSuccess)
	}
	next := nextRefreshAttempt(now, meta)
	if next.IsZero() {
		return "Refresh: Scheduled"
	}
	if !now.Before(next) {
		return "Refresh: Due now"
	}
	return "Refresh: Due in " + compactDuration(next.Sub(now))
}

func compactDuration(d time.Duration) string {
	if d <= 0 {
		return "now"
	}
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Round(time.Hour).Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Round(24*time.Hour).Hours()/24))
	}
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
