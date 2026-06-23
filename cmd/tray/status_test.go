package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
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
	// truncate uses len() which counts bytes, not runes.
	// "héllo" = 6 bytes (h=1, é=2, l=1, l=1, o=1).
	input := "héllo"
	got := truncate(input, 4)
	// s[:3] = "hé" (3 bytes), then + "…" (3 bytes)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Document byte-slicing behavior: truncation happens at byte boundary
	t.Logf("truncate(%q, 4) = %q (len=%d)", input, got, len(got))
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

func TestInspectAuthReadinessBlocksLegacySessionWithoutDataCredential(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})
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
		t.Fatal("data-password action should be enabled for legacy unlock blocker")
	}
	if readiness.action != authActionDisconnect {
		t.Fatalf("signed-in blocked state should offer disconnect, got %s", readiness.action)
	}
}

func TestInspectAuthReadinessReadyForBrowserForkTwoPasswordWithoutDataCredential(t *testing.T) {
	now := time.Date(2026, 6, 23, 18, 0, 0, 0, time.UTC)
	resetRefreshForStatusTest(t, now)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})
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
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})
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
		configJSON: `{"credentialProvider":"pass-cli","dataCredentialProvider":"pass-cli"}`,
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

func TestAuthReadinessDoesNotOverrideActiveTransferStatus(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	if err := config.WriteStatus(config.StatusReport{
		State:  config.StateTransferring,
		LastOp: "upload",
	}); err != nil {
		t.Fatal(err)
	}

	if authReadinessShouldOverrideTransferStatus(authReadiness{
		signedIn: true,
		ready:    true,
	}) {
		t.Fatal("ready auth state must not hide active transfer status")
	}
	if !authReadinessShouldOverrideTransferStatus(authReadiness{
		signedIn: true,
		blocked:  true,
	}) {
		t.Fatal("blocked auth state should override transfer status")
	}
}

func TestTerminalCommandDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	cmd := terminalCommand("echo hello")
	if cmd == nil {
		t.Fatal("expected non-nil command on darwin")
		return
	}
	if len(cmd.Args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", cmd.Args)
	}
	if cmd.Args[0] != "osascript" {
		t.Fatalf("expected osascript, got %q", cmd.Args[0])
	}
	if cmd.Args[1] != "-e" {
		t.Fatalf("expected -e flag, got %q", cmd.Args[1])
	}
	// The AppleScript references a temp script file, not the inline script
	if !strings.Contains(cmd.Args[2], "do script") {
		t.Fatalf("expected 'do script' in AppleScript, got %q", cmd.Args[2])
	}
	if !strings.Contains(cmd.Args[2], "proton-lfs-") {
		t.Fatalf("expected temp file reference in AppleScript, got %q", cmd.Args[2])
	}
}
