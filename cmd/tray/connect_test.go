package main

import (
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
)

func TestShouldOpenInteractiveCredentialStore(t *testing.T) {
	if shouldOpenInteractiveCredentialStore(config.CredentialProviderPassCLI) {
		t.Fatal("pass-cli connect must not open an interactive credential prompt")
	}
	if !shouldOpenInteractiveCredentialStore(config.CredentialProviderGitCredential) {
		t.Fatal("git-credential connect should keep interactive credential setup")
	}
}

func TestWithAuthTraceEnv(t *testing.T) {
	env := withAuthTraceEnv([]string{"EXISTING=1"}, "trace-123")
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"EXISTING=1",
		"PROTON_AUTH_TRACE=1",
		"PROTON_AUTH_TRACE_ID=trace-123",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected auth trace env %q in:\n%s", want, joined)
		}
	}
}

func TestWithAuthTraceEnvSkipsEmptyTraceID(t *testing.T) {
	env := withAuthTraceEnv([]string{"EXISTING=1"}, " ")
	if len(env) != 1 || env[0] != "EXISTING=1" {
		t.Fatalf("expected unchanged env for empty trace id, got %#v", env)
	}
}

func TestIsAuthRateLimitedOutput(t *testing.T) {
	for _, out := range [][]byte{
		[]byte(`[AUTH_TRACE] {"event":"cli.login.failure","errorCode":"RATE_LIMITED"}`),
		[]byte(`[AUTH_TRACE] {"event":"auth.stage.failure","clientCode":"RATE_LIMITED"}`),
		[]byte(`[AUTH_TRACE] {"event":"auth.stage.failure","protonCode":2028}`),
	} {
		if !isAuthRateLimitedOutput(out) {
			t.Fatalf("expected rate-limited output to be detected:\n%s", out)
		}
	}

	if isAuthRateLimitedOutput([]byte(`[AUTH_TRACE] {"errorCode":"AUTH_FAILED"}`)) {
		t.Fatal("non-rate-limited auth failure must not trigger cooldown")
	}
}

func TestActiveAuthRateLimitBlocksFreshLoginStatus(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	now := time.Date(2026, 6, 23, 13, 36, 50, 0, time.UTC)
	if err := config.WriteStatus(config.StatusReport{
		State:     config.StateRateLimited,
		LastOp:    "login",
		ErrorCode: "RATE_LIMITED",
		Timestamp: now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	remaining, blocked := activeAuthRateLimit(now)
	if !blocked {
		t.Fatal("fresh login rate limit should block Connect")
	}
	if remaining != 50*time.Minute {
		t.Fatalf("expected 50m remaining, got %s", remaining)
	}
}

func TestActiveAuthRateLimitIgnoresExpiredOrNonLoginStatus(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	now := time.Date(2026, 6, 23, 13, 36, 50, 0, time.UTC)
	if err := config.WriteStatus(config.StatusReport{
		State:     config.StateRateLimited,
		LastOp:    "download",
		ErrorCode: "RATE_LIMITED",
		Timestamp: now.Add(-10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, blocked := activeAuthRateLimit(now); blocked {
		t.Fatal("transfer rate-limit status must not block login Connect")
	}

	if err := config.WriteStatus(config.StatusReport{
		State:     config.StateRateLimited,
		LastOp:    "login",
		ErrorCode: "RATE_LIMITED",
		Timestamp: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, blocked := activeAuthRateLimit(now); blocked {
		t.Fatal("expired login rate-limit status must not block Connect")
	}
}
