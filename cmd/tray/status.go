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

	"fyne.io/systray"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/bridgeproto"
	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

const pollInterval = 5 * time.Second
const refreshBeforeExpiry = 10 * time.Minute
const refreshRetryInterval = time.Minute
const refreshMaxRetryInterval = 30 * time.Minute
const refreshFallbackInterval = 12 * time.Hour

var (
	stopCh     chan struct{}
	stopOnce   sync.Once
	refreshMu  sync.Mutex
	refreshNow = time.Now
)

var refreshState = sessionRefreshState{}

// sessionChange is the session uid the tray last saw and when it changed
// while the tray ran; a login always yields a new uid.
var sessionChange struct {
	sync.Mutex
	seen bool
	uid  string
	at   time.Time
}

// sessionMetadata is proton-drive-cli's session.json (SessionCredentials in
// src/types/auth.ts).
type sessionMetadata struct {
	UID                 string   `json:"uid"`
	AccessToken         string   `json:"accessToken"`
	RefreshToken        string   `json:"refreshToken"`
	Scopes              []string `json:"scopes"`
	KeyPasswordProvider string   `json:"keyPasswordProvider"`
	KeyPasswordHost     string   `json:"keyPasswordHost"`
	TokenExpiresAt      int64    `json:"tokenExpiresAt"`
	// WrittenAt is the file's modification time: login and every token
	// refresh rewrite session.json.
	WrittenAt time.Time `json:"-"`
}

type authReadiness struct {
	sessionChangedAt time.Time
	statusTitle      string
	sessionTitle     string
	transferTitle    string
	refreshTitle     string
	tooltip          string
	actionTitle      string
	action           authMenuAction
	signedIn         bool
	ready            bool
	blocked          bool
}

type sessionRefreshState struct {
	InProgress     bool
	LastAttempt    time.Time
	LastSuccess    time.Time
	LastError      string
	NextAttempt    time.Time
	ActionRequired bool
	// FailureUID is the session uid LastError belongs to.
	FailureUID          string
	ConsecutiveFailures int
}

type refreshCommandResult struct {
	OK          bool             `json:"ok"`
	Refreshed   bool             `json:"refreshed"`
	State       string           `json:"state"`
	Error       string           `json:"error"`
	ErrorCode   string           `json:"errorCode"`
	Recoverable *bool            `json:"recoverable"`
	RetryAfter  int              `json:"retryAfter"`
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
// status file, the global git LFS registration and the auth readiness.
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

// takeTraySnapshot reads the status file, the git registration and the
// session file once each.
func takeTraySnapshot(now time.Time) traySnapshot {
	report, err := config.ReadStatus()
	return traySnapshot{
		report:     report,
		hasReport:  err == nil,
		lfsEnabled: isLFSEnabled(),
		readiness:  inspectAuthReadiness(now),
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
	case config.StateOK:
		return true
	case config.StateAuthRequired, config.StateCaptcha:
		return s.readiness.sessionChangedAt.After(s.report.Timestamp)
	case config.StateRateLimited:
		return s.report.LastOp == "login" && s.readiness.sessionChangedAt.After(s.report.Timestamp)
	default:
		return false
	}
}

// transferStateIcon maps the adapter's status state to the four embedded
// icons. rate_limited, auth_required and captcha share the error icon: all
// three stop transfers until the user acts, and the tooltip names which.
func transferStateIcon(report config.StatusReport) []byte {
	switch report.State {
	case config.StateOK:
		return iconOK
	case config.StateError, config.StateRateLimited, config.StateAuthRequired, config.StateCaptcha:
		return iconError
	case config.StateTransferring:
		return iconSyncing
	default:
		return iconIdle
	}
}

// maybeRefreshSession refreshes the Proton session token near expiry through
// `proton-drive-cli session refresh` (POST /auth/v4/refresh).
func maybeRefreshSession() {
	meta, ok := readSessionMetadata()
	if !ok || !validSessionShape(meta) {
		return
	}
	now := timeNow()
	decision := decideRefresh(now, meta, currentRefreshState(meta))
	if !decision.Attempt {
		setRefreshNextAttempt(decision.NextAttempt)
		return
	}
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		recordRefreshFailure(now, meta.UID, "proton-drive-cli not found", true)
		refreshTrayState()
		return
	}
	if !markRefreshStarted(now) {
		return
	}
	refreshTrayState()
	go func() {
		performSessionRefresh(driveCLI, meta.UID)
		refreshTrayState()
	}()
}

// performSessionRefresh runs `session refresh --json` bounded by
// driveCLITimeout and records the outcome; a timeout is a recoverable failure
// retried after refreshBackoff.
func performSessionRefresh(driveCLI, uid string) {
	ctx, cancel := context.WithTimeout(context.Background(), driveCLITimeout)
	defer cancel()
	cmd := driveCLICommand(ctx, driveCLI, "session", "refresh", "--json")
	out, err := cmd.CombinedOutput()
	logSubprocessOutput("refresh", out)
	finishedAt := timeNow()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		message := fmt.Sprintf("session refresh timed out after %s", driveCLITimeout)
		recordRefreshFailure(finishedAt, uid, message, true)
		trayLog.Printf("refresh: %s", message)
		return
	}
	result, parsed := parseRefreshCommandResult(out)
	switch {
	case parsed && result.OK && result.Refreshed:
		recordRefreshSuccess(finishedAt)
		trayLog.Print("refresh: session token refreshed")
	case parsed && result.OK:
		// `session refresh --json` reports ok without a refresh only for state
		// no_session (proton-drive-cli src/cli/login.ts).
		recordRefreshFailure(finishedAt, uid, "no usable session; run proton-drive-cli login", false)
		trayLog.Printf("refresh: state %s", result.State)
	case err != nil || parsed:
		message, recoverable := refreshFailureFromCommand(result, parsed, err)
		recordRefreshFailure(finishedAt, uid, message, recoverable)
		if recoverable && result.RetryAfter > 0 {
			deferRefreshUntil(finishedAt.Add(time.Duration(result.RetryAfter) * time.Second))
		}
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
	f, err := os.Open(sf)
	if err != nil {
		return sessionMetadata{}, false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return sessionMetadata{}, false
	}
	// An existing file that does not decode is an invalid session, as in
	// proton-drive-cli's SessionManager.readStoredSession.
	var meta sessionMetadata
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		return sessionMetadata{WrittenAt: info.ModTime()}, true
	}
	meta.WrittenAt = info.ModTime()
	return meta, true
}

// validSessionShape mirrors isSessionCredentials in proton-drive-cli
// src/auth/session.ts.
func validSessionShape(meta sessionMetadata) bool {
	for _, field := range []string{meta.UID, meta.AccessToken, meta.RefreshToken, meta.KeyPasswordHost} {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	return meta.KeyPasswordProvider == config.CredentialProviderPassCLI ||
		meta.KeyPasswordProvider == config.CredentialProviderGitCredential
}

func tokenExpiry(meta sessionMetadata) time.Time {
	if meta.TokenExpiresAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(meta.TokenExpiresAt)
}

// inspectAuthReadiness derives the readiness from the session file and the
// refresh state.
func inspectAuthReadiness(now time.Time) authReadiness {
	meta, hasSession := readSessionMetadata()
	health := currentRefreshState(meta)
	changedAt := observeSessionUID(meta.UID, now)
	base := authReadiness{
		sessionChangedAt: changedAt,
		statusTitle:      "Status: Not connected",
		sessionTitle:     "Session: Not connected",
		transferTitle:    "Transfers: Sign-in required",
		actionTitle:      "Connect to Proton...",
		action:           authActionConnect,
		refreshTitle:     refreshTitle(now, meta, hasSession, health),
		tooltip:          "Proton LFS - Not connected",
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
	base.sessionTitle = "Session: Signed in (" + meta.KeyPasswordProvider + ")"
	if expiry := tokenExpiry(meta); !expiry.IsZero() && !now.Before(expiry) {
		// Bridge commands refresh an expired access token before use
		// (proton-drive-cli src/sdk/client.ts loadUsableSession).
		base.sessionTitle = "Session: Signed in (" + meta.KeyPasswordProvider + "), token refresh due"
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
	currentAuthAction.Store(readiness.action)
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
	refreshState.FailureUID = ""
	return true
}

func recordRefreshSuccess(now time.Time) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastSuccess = now
	refreshState.LastError = ""
	refreshState.ActionRequired = false
	refreshState.FailureUID = ""
	refreshState.ConsecutiveFailures = 0
	if meta, ok := readSessionMetadata(); ok {
		refreshState.NextAttempt = nextRefreshAttempt(now, meta, now)
	} else {
		refreshState.NextAttempt = time.Time{}
	}
}

func recordRefreshFailure(now time.Time, uid string, message string, recoverable bool) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState.InProgress = false
	refreshState.LastAttempt = now
	refreshState.LastError = bridgeproto.Truncate(strings.TrimSpace(message), 120)
	refreshState.ActionRequired = !recoverable
	refreshState.FailureUID = uid
	refreshState.ConsecutiveFailures++
	if recoverable {
		refreshState.NextAttempt = now.Add(refreshBackoff(refreshState.ConsecutiveFailures))
		return
	}
	refreshState.NextAttempt = time.Time{}
}

// deferRefreshUntil moves the next attempt to at least t, the Retry-After of
// a rate-limited refresh (RFC 9110, section 10.2.3).
func deferRefreshUntil(t time.Time) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	if t.After(refreshState.NextAttempt) {
		refreshState.NextAttempt = t
	}
}

// refreshBackoff is binary exponential backoff: refreshRetryInterval doubled
// per further consecutive failure, at most refreshMaxRetryInterval.
func refreshBackoff(failures int) time.Duration {
	delay := refreshRetryInterval
	for i := 1; i < failures && delay < refreshMaxRetryInterval; i++ {
		delay *= 2
	}
	return min(delay, refreshMaxRetryInterval)
}

func decideRefresh(now time.Time, meta sessionMetadata, health sessionRefreshState) refreshDecision {
	if health.InProgress {
		return refreshDecision{}
	}
	if refreshActionRequiredForSession(meta, health) {
		return refreshDecision{}
	}
	if health.LastError != "" && !health.NextAttempt.IsZero() && now.Before(health.NextAttempt) {
		return refreshDecision{NextAttempt: health.NextAttempt}
	}
	next := nextRefreshAttempt(now, meta, health.LastSuccess)
	if !now.Before(next) {
		return refreshDecision{Attempt: true}
	}
	return refreshDecision{NextAttempt: next}
}

// nextRefreshAttempt precedes the token expiry by refreshBeforeExpiry, at most
// half the token lifetime, or follows the last write by refreshFallbackInterval
// when ExpiresIn was absent (optional in Proton's api-auth-types.ts).
func nextRefreshAttempt(now time.Time, meta sessionMetadata, lastSuccess time.Time) time.Time {
	var next time.Time
	expiry := tokenExpiry(meta)
	switch {
	case !expiry.IsZero():
		lead := refreshBeforeExpiry
		if !meta.WrittenAt.IsZero() && expiry.After(meta.WrittenAt) {
			lead = min(lead, expiry.Sub(meta.WrittenAt)/2)
		}
		next = expiry.Add(-lead)
	case !meta.WrittenAt.IsZero():
		next = meta.WrittenAt.Add(refreshFallbackInterval)
	default:
		next = now.Add(refreshFallbackInterval)
	}
	if floor := lastSuccess.Add(refreshRetryInterval); !lastSuccess.IsZero() && next.Before(floor) {
		return floor
	}
	return next
}

// withoutStaleFailure drops a recorded refresh failure of another session uid
// or one older than the last write of session.json (a login or another
// process's refresh).
func withoutStaleFailure(meta sessionMetadata, health sessionRefreshState) sessionRefreshState {
	if health.LastError == "" {
		return health
	}
	otherSession := health.FailureUID != meta.UID
	rewritten := !meta.WrittenAt.IsZero() && meta.WrittenAt.After(health.LastAttempt)
	if !otherSession && !rewritten {
		return health
	}
	health.LastError = ""
	health.ActionRequired = false
	health.FailureUID = ""
	health.NextAttempt = time.Time{}
	health.ConsecutiveFailures = 0
	return health
}

// currentRefreshState applies withoutStaleFailure to the stored refresh state
// and keeps the result.
func currentRefreshState(meta sessionMetadata) sessionRefreshState {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState = withoutStaleFailure(meta, refreshState)
	return refreshState
}

// observeSessionUID records uid and returns when it last changed while the
// tray ran; the first observation has no change time.
func observeSessionUID(uid string, now time.Time) time.Time {
	sessionChange.Lock()
	defer sessionChange.Unlock()
	if sessionChange.seen && uid != sessionChange.uid {
		sessionChange.at = now
	}
	sessionChange.seen = true
	sessionChange.uid = uid
	return sessionChange.at
}

func refreshTitle(now time.Time, meta sessionMetadata, hasSession bool, health sessionRefreshState) string {
	switch {
	case !hasSession:
		return "Refresh: No session"
	case !validSessionShape(meta):
		return "Refresh: Reconnect required"
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
	next := nextRefreshAttempt(now, meta, health.LastSuccess)
	if !now.Before(next) {
		return "Refresh: Due now"
	}
	return "Refresh: Due in " + compactDuration(next.Sub(now))
}

// clearRefreshState keeps InProgress so that a refresh still running for the
// replaced session blocks a second one; its result is dropped by uid.
func clearRefreshState() {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	refreshState = sessionRefreshState{InProgress: refreshState.InProgress}
}

func refreshActionRequiredForSession(meta sessionMetadata, health sessionRefreshState) bool {
	return health.ActionRequired && health.FailureUID != "" && health.FailureUID == meta.UID
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
			return fmt.Sprintf("Proton LFS — Rate Limited: %s%s", bridgeproto.Truncate(report.ErrorDetail, 60), statusMetadataSuffix(report))
		}
		return "Proton LFS — Rate Limit Active" + statusMetadataSuffix(report)
	case report.State == config.StateAuthRequired:
		if report.ErrorDetail != "" {
			return fmt.Sprintf("Proton LFS — Authentication Required: %s", bridgeproto.Truncate(report.ErrorDetail, 60))
		}
		return "Proton LFS — Authentication Required"
	case report.State == config.StateCaptcha:
		return "Proton LFS — CAPTCHA Verification Required"
	case report.State == config.StateError:
		if report.ErrorCode != "" && report.ErrorDetail != "" {
			return fmt.Sprintf("Proton LFS — %s: %s%s", report.ErrorCode, bridgeproto.Truncate(report.ErrorDetail, 50), statusMetadataSuffix(report))
		}
		if report.Error != "" {
			return fmt.Sprintf("Proton LFS — Error: %s%s", bridgeproto.Truncate(report.Error, 60), statusMetadataSuffix(report))
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
