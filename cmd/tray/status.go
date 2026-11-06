package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/systray"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
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
	InProgress      bool
	LastAttempt     time.Time
	LastSuccess     time.Time
	LastError       string
	NextAttempt     time.Time
	ActionRequired  bool
	ActionSessionID string
}

type refreshCommandResult struct {
	OK          bool             `json:"ok"`
	Refreshed   bool             `json:"refreshed"`
	State       string           `json:"state"`
	Error       string           `json:"error"`
	ErrorCode   string           `json:"errorCode"`
	Recoverable *bool            `json:"recoverable"`
	StatusCode  int              `json:"statusCode"`
	ProtonCode  int              `json:"protonCode"`
	ProtonError string           `json:"protonError"`
	Action      string           `json:"action"`
	Details     *json.RawMessage `json:"details"`
}

type refreshDecision struct {
	Attempt     bool
	NextAttempt time.Time
}

// traySnapshot is everything one poll tick needs, read once: the adapter's
// status file, the global git LFS registration and the derived auth
// readiness.
type traySnapshot struct {
	report     config.StatusReport
	hasReport  bool
	lfsEnabled bool
	readiness  authReadiness
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

	refreshTrayState()

	for {
		select {
		case <-ticker.C:
			refreshTrayState()
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

// takeTraySnapshot reads the status file and the git registration once and
// derives the auth readiness from the same git values.
func takeTraySnapshot(now time.Time) traySnapshot {
	report, err := config.ReadStatus()
	lfs := readGitLFSConfig()
	return traySnapshot{
		report:     report,
		hasReport:  err == nil,
		lfsEnabled: lfs.StandaloneAgent == gitStandaloneAgent,
		readiness:  inspectAuthReadinessWith(now, lfs),
	}
}

// refreshTrayState recomputes the tray from one snapshot and applies it:
// menu labels, the Register checkmark, and exactly one icon and tooltip.
func refreshTrayState() {
	applyTraySnapshot(takeTraySnapshot(timeNow()))
}

func applyTraySnapshot(s traySnapshot) {
	applyAuthReadinessLabels(s.readiness)
	applyRegisterStatus(s.lfsEnabled)
	icon, tooltip := resolveTrayIcon(s)
	systray.SetIcon(icon)
	systray.SetTemplateIcon(icon, icon)
	systray.SetTooltip(tooltip)
}

// resolveTrayIcon picks the icon and tooltip for one snapshot. Auth
// readiness wins while sign-in is missing or blocked and while no transfer
// is active or failed; otherwise the adapter's last transfer state shows.
func resolveTrayIcon(s traySnapshot) ([]byte, string) {
	if authReadinessOverridesTransferStatus(s) {
		switch {
		case s.readiness.ready:
			return iconOK, s.readiness.tooltip
		case s.readiness.blocked:
			return iconError, s.readiness.tooltip
		default:
			return iconIdle, s.readiness.tooltip
		}
	}
	return transferStateIcon(s.report), statusTooltip(s.report)
}

func authReadinessOverridesTransferStatus(s traySnapshot) bool {
	if s.readiness.blocked || !s.readiness.signedIn || !s.hasReport {
		return true
	}
	switch s.report.State {
	case config.StateIdle, config.StateOK:
		return true
	default:
		return false
	}
}

// transferStateIcon maps the adapter's status state to the four embedded
// icons. rate_limited, auth_required and captcha share the error icon: all
// three stop transfers until the user acts, and the tooltip names which.
func transferStateIcon(report config.StatusReport) []byte {
	switch report.State {
	case config.StateIdle, config.StateOK:
		return iconOK
	case config.StateError, config.StateRateLimited, config.StateAuthRequired, config.StateCaptcha:
		return iconError
	case config.StateTransferring:
		return iconSyncing
	default:
		return iconIdle
	}
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
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		recordRefreshFailure(now, meta.SessionID, "proton-drive-cli not found", true)
		refreshTrayState()
		return
	}
	if !markRefreshStarted(now) {
		return
	}
	refreshTrayState()
	go func() {
		performSessionRefresh(driveCLI, meta.SessionID)
		refreshTrayState()
	}()
}

// performSessionRefresh runs `session refresh --json` bounded by
// driveCLITimeout and records the outcome. A timeout is a recoverable
// failure: the state leaves "in progress" and the next attempt is scheduled
// after refreshRetryInterval.
func performSessionRefresh(driveCLI, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), driveCLITimeout)
	defer cancel()
	cmd := driveCLICommand(ctx, driveCLI, "session", "refresh", "--json")
	out, err := cmd.CombinedOutput()
	logSubprocessOutput("refresh", out)
	finishedAt := timeNow()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		message := fmt.Sprintf("session refresh timed out after %s", driveCLITimeout)
		recordRefreshFailure(finishedAt, sessionID, message, true)
		trayLog.Printf("refresh: %s", message)
		return
	}
	result, parsed := parseRefreshCommandResult(out)
	switch {
	case parsed && result.OK:
		recordRefreshSuccess(finishedAt)
		trayLog.Print("refresh: session token refreshed")
	case err != nil || parsed:
		message, recoverable := refreshFailureFromCommand(result, parsed, err)
		recordRefreshFailure(finishedAt, sessionID, message, recoverable)
		trayLog.Printf("refresh: failed: %s", message)
	default:
		recordRefreshSuccess(finishedAt)
		trayLog.Print("refresh: session token refreshed")
	}
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

// hasConfiguredDataCredential reports whether a mailbox/data password
// provider is configured in prefs, the environment or the git LFS args.
func hasConfiguredDataCredential(lfs gitLFSConfig) bool {
	prefs := config.LoadPrefs()
	if strings.TrimSpace(prefs.DataCredentialProvider) != "" {
		return true
	}
	if config.EnvTrim(config.EnvDataCredentialProvider) != "" {
		return true
	}
	return parseFlagValue(lfs.TransferArgs, "--data-credential-provider") != ""
}

func browserForkKeyPasswordPersisted(meta sessionMetadata) bool {
	return meta.AuthMode == "browser-fork" && meta.KeyPasswordPersisted
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

// inspectAuthReadiness derives the readiness from the session file, the
// refresh state and the current git registration.
func inspectAuthReadiness(now time.Time) authReadiness {
	return inspectAuthReadinessWith(now, readGitLFSConfig())
}

func inspectAuthReadinessWith(now time.Time, lfs gitLFSConfig) authReadiness {
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
	if refreshActionRequiredForSession(meta, health) {
		base.statusTitle = "Status: Setup needed"
		base.sessionTitle = "Session: Refresh failed"
		base.transferTitle = "Transfers: Reconnect required"
		base.tooltip = "Proton LFS - Setup needed: session refresh failed"
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
	if meta.AuthMode == "browser-fork" && !meta.KeyPasswordPersisted {
		base.statusTitle = "Status: Setup needed"
		base.transferTitle = "Transfers: Reconnect required"
		base.tooltip = "Proton LFS - Setup needed: browser-fork key password missing"
		base.blocked = true
		return base
	}
	if !browserForkKeyPasswordPersisted(meta) && !hasConfiguredDataCredential(lfs) {
		base.statusTitle = "Status: Setup needed"
		base.transferTitle = "Transfers: Data password needed"
		base.dataPasswordActive = true
		base.tooltip = "Proton LFS - Setup needed: local unlock material required"
		base.blocked = true
		return base
	}
	base.statusTitle = "Status: Ready"
	base.transferTitle = "Transfers: Ready"
	base.tooltip = "Proton LFS - Ready"
	base.ready = true
	return base
}

// applyAuthReadinessLabels updates the menu items derived from readiness.
// The icon is set by applyTraySnapshot.
func applyAuthReadinessLabels(readiness authReadiness) {
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
	refreshState.ActionRequired = false
	refreshState.ActionSessionID = ""
	return true
}

func recordRefreshSuccess(now time.Time) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastSuccess = now
	refreshState.LastError = ""
	refreshState.ActionRequired = false
	refreshState.ActionSessionID = ""
	if meta, ok := readSessionMetadata(); ok {
		refreshState.NextAttempt = nextRefreshAttempt(now, meta)
	} else {
		refreshState.NextAttempt = time.Time{}
	}
}

func recordRefreshFailure(now time.Time, sessionID string, message string, recoverable bool) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastAttempt = now
	refreshState.LastError = truncate(strings.TrimSpace(message), 120)
	refreshState.ActionRequired = !recoverable
	refreshState.ActionSessionID = ""
	if recoverable {
		refreshState.NextAttempt = now.Add(refreshRetryInterval)
		return
	}
	refreshState.ActionSessionID = sessionID
	refreshState.NextAttempt = time.Time{}
}

func decideRefresh(now time.Time, meta sessionMetadata, health sessionRefreshState) refreshDecision {
	if health.InProgress {
		return refreshDecision{}
	}
	if refreshActionRequiredForSession(meta, health) {
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
	case refreshActionRequiredForSession(meta, health):
		return "Refresh: Reconnect required"
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

func clearRefreshState() {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState = sessionRefreshState{}
}

func refreshActionRequiredForSession(meta sessionMetadata, health sessionRefreshState) bool {
	return health.ActionRequired && health.ActionSessionID != "" && health.ActionSessionID == meta.SessionID
}

func parseRefreshCommandResult(out []byte) (refreshCommandResult, bool) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return refreshCommandResult{}, false
	}
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var result refreshCommandResult
		if err := json.Unmarshal([]byte(line), &result); err == nil && result.State != "" {
			return result, true
		}
	}
	var result refreshCommandResult
	if err := json.Unmarshal([]byte(text), &result); err == nil && result.State != "" {
		return result, true
	}
	return refreshCommandResult{}, false
}

func refreshFailureFromCommand(result refreshCommandResult, parsed bool, err error) (string, bool) {
	if !parsed {
		if err != nil {
			return err.Error(), true
		}
		return "refresh failed", true
	}
	recoverable := true
	if result.Recoverable != nil {
		recoverable = *result.Recoverable
	}
	summary := strings.TrimSpace(result.ProtonError)
	if summary == "" {
		summary = strings.TrimSpace(result.Error)
	}
	if summary == "" && err != nil {
		summary = err.Error()
	}
	if summary == "" {
		summary = "refresh failed"
	}

	var details []string
	if result.ErrorCode != "" {
		details = append(details, "code="+result.ErrorCode)
	}
	if result.StatusCode > 0 {
		details = append(details, fmt.Sprintf("status=%d", result.StatusCode))
	}
	if result.ProtonCode > 0 {
		details = append(details, fmt.Sprintf("proton=%d", result.ProtonCode))
	}
	if len(details) > 0 {
		summary = fmt.Sprintf("%s (%s)", summary, strings.Join(details, " "))
	}
	if result.Action != "" {
		summary = summary + "; " + result.Action
	}
	return summary, recoverable
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

// truncate shortens s to limit runes, ending with an ellipsis. It counts
// runes, not bytes, so the result is always valid UTF-8.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}
