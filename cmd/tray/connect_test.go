package main

import (
	"io"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
)

func setupTrayLogForTest(t *testing.T) {
	t.Helper()
	original := trayLog
	trayLog = log.New(io.Discard, "", 0)
	t.Cleanup(func() {
		trayLog = original
	})
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

func TestRedactSubprocessOutput(t *testing.T) {
	input := `{"AccessToken":"access-token","RefreshToken":"refresh-token","UID":"uid-123"} Bearer secret-token password=hunter2`
	got := redactSubprocessOutput(input)
	for _, forbidden := range []string{"access-token", "refresh-token", "uid-123", "secret-token", "hunter2"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("redacted output leaked %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected redaction marker in %s", got)
	}
}

func TestBuildTrayLoginArgsUsesBrowserForkKeyPasswordProviderOnly(t *testing.T) {
	setupTrayLogForTest(t)
	args, ok := buildTrayLoginArgs(config.CredentialProviderPassCLI)
	if !ok {
		t.Fatal("expected login args to be built")
	}

	want := []string{"--key-password-provider", config.CredentialProviderPassCLI}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected login args:\n got: %#v\nwant: %#v", args, want)
	}
	for _, forbidden := range []string{"--auth-mode", "--credential-provider"} {
		for _, arg := range args {
			if arg == forbidden {
				t.Fatalf("login args must not contain %s: %#v", forbidden, args)
			}
		}
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
