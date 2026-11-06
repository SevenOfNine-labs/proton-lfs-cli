package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

func writeSessionForStatusTest(t *testing.T, home string, meta sessionMetadata) {
	t.Helper()
	dir := filepath.Join(home, ".proton-drive-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func resetRefreshForStatusTest(t *testing.T, now time.Time) {
	t.Helper()
	origNow := refreshNow
	refreshNow = func() time.Time { return now }
	refreshMu.Lock()
	origState := refreshState
	refreshState = sessionRefreshState{}
	refreshMu.Unlock()
	t.Cleanup(func() {
		refreshNow = origNow
		refreshMu.Lock()
		refreshState = origState
		refreshMu.Unlock()
	})
}

func TestRelativeTime(t *testing.T) {
	cases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"just now", 0, "just now"},
		{"30 seconds", 30 * time.Second, "just now"},
		{"59 seconds", 59 * time.Second, "just now"},
		{"1 minute", 1 * time.Minute, "1m ago"},
		{"5 minutes", 5 * time.Minute, "5m ago"},
		{"30 minutes", 30 * time.Minute, "30m ago"},
		{"59 minutes", 59 * time.Minute, "59m ago"},
		{"1 hour", 1 * time.Hour, "1h ago"},
		{"2 hours", 2 * time.Hour, "2h ago"},
		{"23 hours", 23 * time.Hour, "23h ago"},
		{"1 day", 24 * time.Hour, "1d ago"},
		{"2 days", 48 * time.Hour, "2d ago"},
		{"7 days", 7 * 24 * time.Hour, "7d ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			past := time.Now().Add(-tc.duration)
			got := relativeTime(past)
			if got != tc.expected {
				t.Fatalf("relativeTime(%v ago) = %q, expected %q", tc.duration, got, tc.expected)
			}
		})
	}
}

func TestRelativeTimeFutureTimestamp(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	got := relativeTime(future)
	if got != "just now" {
		t.Fatalf("expected 'just now' for future time, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		limit    int
		expected string
	}{
		{"short", "abc", 10, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"truncated", "abcdefgh", 5, "abcd…"},
		{"limit=1", "abc", 1, "…"},
		{"empty", "", 5, ""},
		{"boundary", "abcde", 6, "abcde"},
		{"boundary-exact", "abcdef", 6, "abcdef"},
		{"long", "this is a long string that should be truncated", 10, "this is a…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.limit)
			if got != tc.expected {
				t.Fatalf("truncate(%q, %d) = %q, expected %q", tc.input, tc.limit, got, tc.expected)
			}
		})
	}
}

func TestTruncateMultibyteInput(t *testing.T) {
	cases := []struct {
		input string
		limit int
		want  string
	}{
		{"héllo wörld", 4, "hél…"},
		{"héllo", 5, "héllo"},
		{"日本語のテキスト", 4, "日本語…"},
		{"😀😀😀", 2, "😀…"},
	}
	for _, tc := range cases {
		got := truncate(tc.input, tc.limit)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.limit, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) = %q is not valid UTF-8", tc.input, tc.limit, got)
		}
	}
}

func TestTransferStatusTextWithRetryMetadata(t *testing.T) {
	report := config.StatusReport{
		State:     config.StateError,
		LastOp:    "upload",
		Error:     "drive service is unavailable",
		ErrorCode: "server_error",
		Retryable: true,
		Temporary: true,
		Timestamp: time.Now().Add(-1 * time.Minute),
	}

	got := transferStatusText(report)
	for _, want := range []string{
		"upload",
		"drive service is unavailable",
		"code=server_error",
		"retryable",
		"temporary",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("transferStatusText missing %q: %s", want, got)
		}
	}
}

func TestStatusTooltipWithTemporaryRateLimit(t *testing.T) {
	report := config.StatusReport{
		State:       config.StateRateLimited,
		ErrorDetail: "Wait before retrying operations",
		ErrorCode:   "rate_limited",
		Temporary:   true,
	}

	got := statusTooltip(report)
	for _, want := range []string{
		"Rate Limited",
		"Wait before retrying operations",
		"temporary",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("statusTooltip missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "retryable") {
		t.Fatalf("rate-limit tooltip must not show adapter retryable: %s", got)
	}
}

func TestStatusTooltipAuthBlockerOmitsRetryMetadata(t *testing.T) {
	report := config.StatusReport{
		State:       config.StateAuthRequired,
		ErrorDetail: "Run proton-drive login before Git LFS transfer",
		ErrorCode:   "auth_required",
	}

	got := statusTooltip(report)
	if !strings.Contains(got, "Authentication Required") {
		t.Fatalf("statusTooltip missing auth state: %s", got)
	}
	if strings.Contains(got, "retryable") || strings.Contains(got, "temporary") {
		t.Fatalf("auth tooltip must not show retry metadata: %s", got)
	}
}

func TestSessionFilePath(t *testing.T) {
	got := sessionFilePath()
	if got == "" {
		t.Fatal("expected non-empty session file path")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}
	expected := filepath.Join(home, ".proton-drive-cli", "session.json")
	if got != expected {
		t.Fatalf("sessionFilePath() = %q, expected %q", got, expected)
	}
}

func TestInspectAuthReadinessBlocksPasswordLoginSessionWithoutDataCredential(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"keyPasswordProvider":"pass-cli"}`})
	setupGitConfig(t, "")
	writeSessionForStatusTest(t, home, sessionMetadata{
		SessionID:      "session-id",
		UID:            "uid-123",
		AccessToken:    "access",
		RefreshToken:   "refresh",
		Scopes:         []string{"drive"},
		PasswordMode:   2,
		TokenExpiresAt: now.Add(24 * time.Hour).UnixMilli(),
	})

	readiness := inspectAuthReadiness(now)
	if !readiness.blocked || readiness.ready {
		t.Fatalf("expected blocked readiness, got %#v", readiness)
	}
	if readiness.statusTitle != "Status: Setup needed" {
		t.Fatalf("unexpected status title: %s", readiness.statusTitle)
	}
	if readiness.transferTitle != "Transfers: Data password needed" {
		t.Fatalf("unexpected transfer title: %s", readiness.transferTitle)
	}
	if !readiness.dataPasswordActive {
		t.Fatal("data-password action should be enabled for a password-login session without stored unlock material")
	}
	if readiness.action != authActionDisconnect {
		t.Fatalf("signed-in blocked state should offer disconnect, got %s", readiness.action)
	}
}

func TestInspectAuthReadinessReadyForBrowserForkTwoPasswordWithoutDataCredential(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"keyPasswordProvider":"pass-cli"}`})
	setupGitConfig(t, "")
	writeSessionForStatusTest(t, home, sessionMetadata{
		SessionID:            "session-id",
		UID:                  "uid-123",
		AccessToken:          "access",
		RefreshToken:         "refresh",
		Scopes:               []string{"drive"},
		PasswordMode:         2,
		AuthMode:             "browser-fork",
		KeyPasswordPersisted: true,
		TokenExpiresAt:       now.Add(24 * time.Hour).UnixMilli(),
	})

	readiness := inspectAuthReadiness(now)
	if !readiness.ready || readiness.blocked {
		t.Fatalf("expected ready readiness, got %#v", readiness)
	}
	if readiness.statusTitle != "Status: Ready" {
		t.Fatalf("unexpected status title: %s", readiness.statusTitle)
	}
	if readiness.transferTitle != "Transfers: Ready" {
		t.Fatalf("unexpected transfer title: %s", readiness.transferTitle)
	}
	if readiness.dataPasswordActive {
		t.Fatal("data-password action should not be active for browser-fork key material")
	}
}

func TestInspectAuthReadinessBlocksBrowserForkMissingKeyPassword(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"keyPasswordProvider":"pass-cli"}`})
	setupGitConfig(t, "")
	writeSessionForStatusTest(t, home, sessionMetadata{
		SessionID:      "session-id",
		UID:            "uid-123",
		AccessToken:    "access",
		RefreshToken:   "refresh",
		Scopes:         []string{"drive"},
		PasswordMode:   1,
		AuthMode:       "browser-fork",
		TokenExpiresAt: now.Add(24 * time.Hour).UnixMilli(),
	})

	readiness := inspectAuthReadiness(now)
	if !readiness.blocked || readiness.ready {
		t.Fatalf("expected blocked readiness, got %#v", readiness)
	}
	if readiness.transferTitle != "Transfers: Reconnect required" {
		t.Fatalf("unexpected transfer title: %s", readiness.transferTitle)
	}
	if readiness.dataPasswordActive {
		t.Fatal("data-password action should not be active for missing key-password blocker")
	}
}

func TestInspectAuthReadinessReadyWithConfiguredDataCredential(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{
		configJSON: `{"keyPasswordProvider":"pass-cli","dataCredentialProvider":"pass-cli"}`,
	})
	setupGitConfig(t, "")
	writeSessionForStatusTest(t, home, sessionMetadata{
		SessionID:            "session-id",
		UID:                  "uid-123",
		AccessToken:          "access",
		RefreshToken:         "refresh",
		Scopes:               []string{"drive"},
		PasswordMode:         2,
		AuthMode:             "browser-fork",
		KeyPasswordPersisted: true,
		TokenExpiresAt:       now.Add(24 * time.Hour).UnixMilli(),
	})

	readiness := inspectAuthReadiness(now)
	if !readiness.ready || readiness.blocked {
		t.Fatalf("expected ready readiness, got %#v", readiness)
	}
	if readiness.transferTitle != "Transfers: Ready" {
		t.Fatalf("unexpected transfer title: %s", readiness.transferTitle)
	}
}

func TestDecideRefreshUsesTokenExpiry(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	meta := sessionMetadata{TokenExpiresAt: now.Add(24 * time.Hour).UnixMilli()}

	decision := decideRefresh(now, meta, sessionRefreshState{})
	if decision.Attempt {
		t.Fatal("fresh token should not refresh immediately")
	}
	wantNext := now.Add(24 * time.Hour).Add(-refreshBeforeExpiry)
	if !decision.NextAttempt.Equal(wantNext) {
		t.Fatalf("next attempt = %s, want %s", decision.NextAttempt, wantNext)
	}

	nearExpiry := sessionMetadata{TokenExpiresAt: now.Add(5 * time.Minute).UnixMilli()}
	if decision := decideRefresh(now, nearExpiry, sessionRefreshState{}); !decision.Attempt {
		t.Fatalf("near-expiry token should refresh, got %#v", decision)
	}
}

func TestDecideRefreshHonorsRetryBackoff(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	meta := sessionMetadata{TokenExpiresAt: now.Add(5 * time.Minute).UnixMilli()}
	health := sessionRefreshState{LastError: "network", NextAttempt: now.Add(time.Minute)}

	decision := decideRefresh(now, meta, health)
	if decision.Attempt {
		t.Fatal("refresh should wait for retry backoff")
	}
	if !decision.NextAttempt.Equal(health.NextAttempt) {
		t.Fatalf("next attempt = %s, want %s", decision.NextAttempt, health.NextAttempt)
	}
}

func TestParseRefreshCommandResultAndTerminalFailure(t *testing.T) {
	out := []byte(`debug line
{"ok":false,"state":"refresh_failed","error":"Request failed with status 400","errorCode":"API_ERROR","recoverable":false,"statusCode":400,"protonCode":10013,"protonError":"Invalid refresh token","action":"stop: refresh token rejected or session invalid; run browser login after offline gates pass"}`)

	result, ok := parseRefreshCommandResult(out)
	if !ok {
		t.Fatal("expected refresh JSON to parse")
	}
	if result.Recoverable == nil || *result.Recoverable {
		t.Fatalf("expected terminal recoverable=false result, got %#v", result.Recoverable)
	}
	message, recoverable := refreshFailureFromCommand(result, true, errors.New("exit status 1"))
	if recoverable {
		t.Fatalf("expected terminal refresh failure, got message %q", message)
	}
	for _, want := range []string{
		"Invalid refresh token",
		"code=API_ERROR",
		"status=400",
		"proton=10013",
		"stop:",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("failure summary missing %q: %s", want, message)
		}
	}
}

func TestDecideRefreshStopsAfterTerminalFailureForSameSession(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	meta := sessionMetadata{
		SessionID:      "session-1",
		TokenExpiresAt: now.Add(5 * time.Minute).UnixMilli(),
	}
	health := sessionRefreshState{
		LastError:       "Invalid refresh token",
		ActionRequired:  true,
		ActionSessionID: "session-1",
	}

	decision := decideRefresh(now, meta, health)
	if decision.Attempt {
		t.Fatal("terminal same-session refresh failure must not retry")
	}
	if got := refreshTitle(now, meta, true, health); got != "Refresh: Reconnect required" {
		t.Fatalf("refresh title = %q, want reconnect required", got)
	}
}

func TestDecideRefreshAllowsNewSessionAfterTerminalFailure(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	meta := sessionMetadata{
		SessionID:      "session-2",
		TokenExpiresAt: now.Add(5 * time.Minute).UnixMilli(),
	}
	health := sessionRefreshState{
		LastError:       "Invalid refresh token",
		ActionRequired:  true,
		ActionSessionID: "session-1",
	}

	decision := decideRefresh(now, meta, health)
	if !decision.Attempt {
		t.Fatalf("new session should not inherit terminal refresh failure, got %#v", decision)
	}
}

func TestInspectAuthReadinessBlocksTerminalRefreshFailure(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"keyPasswordProvider":"pass-cli"}`})
	setupGitConfig(t, "")
	writeSessionForStatusTest(t, home, sessionMetadata{
		SessionID:            "session-id",
		UID:                  "uid-123",
		AccessToken:          "access",
		RefreshToken:         "refresh",
		Scopes:               []string{"drive"},
		PasswordMode:         1,
		AuthMode:             "browser-fork",
		KeyPasswordPersisted: true,
		TokenExpiresAt:       now.Add(24 * time.Hour).UnixMilli(),
	})
	refreshMu.Lock()
	refreshState = sessionRefreshState{
		LastError:       "Invalid refresh token",
		ActionRequired:  true,
		ActionSessionID: "session-id",
	}
	refreshMu.Unlock()

	readiness := inspectAuthReadiness(now)
	if !readiness.blocked || readiness.ready {
		t.Fatalf("expected blocked readiness, got %#v", readiness)
	}
	if readiness.sessionTitle != "Session: Refresh failed" {
		t.Fatalf("unexpected session title: %s", readiness.sessionTitle)
	}
	if readiness.transferTitle != "Transfers: Reconnect required" {
		t.Fatalf("unexpected transfer title: %s", readiness.transferTitle)
	}
}

func TestAuthReadinessDoesNotOverrideActiveTransferStatus(t *testing.T) {
	transferring := traySnapshot{
		report:    config.StatusReport{State: config.StateTransferring, LastOp: "upload"},
		hasReport: true,
		readiness: authReadiness{signedIn: true, ready: true, tooltip: "Proton LFS - Ready"},
	}
	if authReadinessOverridesTransferStatus(transferring) {
		t.Fatal("ready auth state must not hide active transfer status")
	}
	if icon, tooltip := resolveTrayIcon(transferring); string(icon) != string(iconSyncing) || tooltip != "Proton LFS — Uploading…" {
		t.Fatalf("active upload should show syncing icon, got tooltip %q", tooltip)
	}

	blocked := transferring
	blocked.readiness = authReadiness{signedIn: true, blocked: true, tooltip: "Proton LFS - Setup needed"}
	if !authReadinessOverridesTransferStatus(blocked) {
		t.Fatal("blocked auth state should override transfer status")
	}
	if icon, tooltip := resolveTrayIcon(blocked); string(icon) != string(iconError) || tooltip != "Proton LFS - Setup needed" {
		t.Fatalf("blocked auth should show error icon, got tooltip %q", tooltip)
	}
}

func TestPerformSessionRefreshTimeoutIsRecoverable(t *testing.T) {
	setupTrayLogForTest(t)
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	setupFakeHome(t, fakeHomeOpts{})

	slow := filepath.Join(t.TempDir(), "proton-drive-cli")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	origTimeout := driveCLITimeout
	driveCLITimeout = 200 * time.Millisecond
	t.Cleanup(func() { driveCLITimeout = origTimeout })

	if !markRefreshStarted(now) {
		t.Fatal("precondition: refresh not in progress")
	}
	start := time.Now()
	performSessionRefresh(slow, "session-1")
	if elapsed := time.Since(start); elapsed > driveCLITimeout+subprocessWaitDelay {
		t.Fatalf("refresh took %s; the timeout must bound it", elapsed)
	}

	state := snapshotRefreshState()
	if state.InProgress {
		t.Fatal("refresh must not stay in progress after a timeout")
	}
	if state.ActionRequired || state.ActionSessionID != "" {
		t.Fatalf("timeout must be recoverable, got %#v", state)
	}
	if !strings.Contains(state.LastError, "timed out") {
		t.Fatalf("LastError = %q, want timeout message", state.LastError)
	}
	if want := now.Add(refreshRetryInterval); !state.NextAttempt.Equal(want) {
		t.Fatalf("NextAttempt = %s, want %s", state.NextAttempt, want)
	}
	meta := sessionMetadata{SessionID: "session-1", TokenExpiresAt: now.Add(5 * time.Minute).UnixMilli()}
	if decision := decideRefresh(now.Add(2*refreshRetryInterval), meta, state); !decision.Attempt {
		t.Fatalf("refresh must be retried after the backoff, got %#v", decision)
	}
}

func TestPerformSessionRefreshRecordsResult(t *testing.T) {
	setupTrayLogForTest(t)
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	setupFakeHome(t, fakeHomeOpts{})

	fake := filepath.Join(t.TempDir(), "proton-drive-cli")
	script := "#!/bin/sh\necho '{\"ok\":false,\"state\":\"refresh_failed\",\"error\":\"Invalid refresh token\",\"recoverable\":false}'\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if !markRefreshStarted(now) {
		t.Fatal("precondition: refresh not in progress")
	}
	performSessionRefresh(fake, "session-1")
	state := snapshotRefreshState()
	if state.InProgress || !state.ActionRequired || state.ActionSessionID != "session-1" {
		t.Fatalf("terminal failure not recorded: %#v", state)
	}
}

func TestResolveTrayIconPerState(t *testing.T) {
	ready := authReadiness{signedIn: true, ready: true, tooltip: "Proton LFS - Ready"}
	notConnected := authReadiness{tooltip: "Proton LFS - Not connected"}
	cases := []struct {
		name     string
		snapshot traySnapshot
		wantIcon []byte
		wantTip  string
	}{
		{"no status file", traySnapshot{readiness: ready}, iconOK, "Proton LFS - Ready"},
		{"not signed in hides ok transfer", traySnapshot{report: config.StatusReport{State: config.StateOK}, hasReport: true, readiness: notConnected}, iconIdle, "Proton LFS - Not connected"},
		{"error transfer", traySnapshot{report: config.StatusReport{State: config.StateError, Error: "boom"}, hasReport: true, readiness: ready}, iconError, "Proton LFS — Error: boom"},
		{"rate limited shares error icon", traySnapshot{report: config.StatusReport{State: config.StateRateLimited}, hasReport: true, readiness: ready}, iconError, "Proton LFS — Rate Limit Active"},
		{"auth required shares error icon", traySnapshot{report: config.StatusReport{State: config.StateAuthRequired}, hasReport: true, readiness: ready}, iconError, "Proton LFS — Authentication Required"},
		{"captcha shares error icon", traySnapshot{report: config.StatusReport{State: config.StateCaptcha}, hasReport: true, readiness: ready}, iconError, "Proton LFS — CAPTCHA Verification Required"},
		{"download in progress", traySnapshot{report: config.StatusReport{State: config.StateTransferring, LastOp: "download"}, hasReport: true, readiness: ready}, iconSyncing, "Proton LFS — Downloading…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			icon, tip := resolveTrayIcon(tc.snapshot)
			if string(icon) != string(tc.wantIcon) {
				t.Fatalf("wrong icon for %s", tc.name)
			}
			if tip != tc.wantTip {
				t.Fatalf("tooltip = %q, want %q", tip, tc.wantTip)
			}
		})
	}
}

func TestTakeTraySnapshotReadsEachSourceOnce(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "[lfs]\n\tstandalonetransferagent = proton\n")
	queries := countGitConfigQueries(t)
	invalidateGitLFSConfig()

	s := takeTraySnapshot(now)
	if !s.lfsEnabled {
		t.Fatal("snapshot should see the registration")
	}
	if s.hasReport {
		t.Fatal("no status file was written")
	}
	if *queries != 1 {
		t.Fatalf("git queried %d times for one snapshot, want 1", *queries)
	}
}
