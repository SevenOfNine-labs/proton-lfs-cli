package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/bridgeproto"
	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
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
	got := bridgeproto.RedactText(input)
	for _, forbidden := range []string{"access-token", "refresh-token", "uid-123", "secret-token", "hunter2"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("redacted output leaked %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected redaction marker in %s", got)
	}
}

func TestProtonDriveLoginWithTracePassesArgsAndTraceEnv(t *testing.T) {
	setupTrayLogForTest(t)
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	script := filepath.Join(t.TempDir(), "proton-drive-cli")
	body := "#!/bin/sh\necho \"args=$*\"\necho \"trace=$PROTON_AUTH_TRACE_ID\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := protonDriveLoginWithTrace(script, config.CredentialProviderGitCredential, "trace-1",
		"--key-password-provider", config.CredentialProviderGitCredential)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, want := range []string{"args=login --key-password-provider git-credential -q", "trace=trace-1"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("output missing %q:\n%s", want, out)
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

func TestRedactSubprocessOutputRedactsSecretBeforeLaterMarker(t *testing.T) {
	// A JSON token before a Bearer marker is redacted as well.
	input := `{"accessToken":"LEAKED-SECRET"} ... Bearer xyz`
	got := bridgeproto.RedactText(input)
	if strings.Contains(got, "LEAKED-SECRET") {
		t.Fatalf("access token leaked: %s", got)
	}
	if strings.Contains(got, "xyz") {
		t.Fatalf("bearer token leaked: %s", got)
	}
	if !strings.Contains(got, `"accessToken":"[redacted]"`) || !strings.Contains(got, "Bearer [redacted]") {
		t.Fatalf("expected both values redacted in place: %s", got)
	}
}

func TestRunLoggedCommandHandlesLinesLongerThanScannerLimit(t *testing.T) {
	setupTrayLogForTest(t)
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	// 300 KiB on one line exceeds bufio.MaxScanTokenSize (64 KiB).
	script := filepath.Join(t.TempDir(), "noisy.sh")
	body := "#!/bin/sh\n" +
		"head -c 307200 /dev/zero | tr '\\0' 'x'\n" +
		"printf '\\nsecond line\\n'\n" +
		"printf 'stderr line\\n' >&2\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		defer close(done)
		out, err = runLoggedCommand("test", exec.Command(script))
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("runLoggedCommand deadlocked on a long line")
	}
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var longest int
	var sawSecond, sawStderr bool
	for _, line := range lines {
		if len(line) > longest {
			longest = len(line)
		}
		sawSecond = sawSecond || line == "second line"
		sawStderr = sawStderr || line == "stderr line"
	}
	if longest != 307200 {
		t.Fatalf("longest line = %d bytes, want 307200 (line kept whole)", longest)
	}
	if !sawSecond || !sawStderr {
		t.Fatalf("expected stdout and stderr lines after the long line:\n%d lines", len(lines))
	}
}

func TestLineLoggerFlushesTrailingPartialLine(t *testing.T) {
	setupTrayLogForTest(t)
	var out bytes.Buffer
	var mu sync.Mutex
	w := newLineLogger("test", &out, &mu)
	if _, err := w.Write([]byte("first\nsec")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("ond\ntail")); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if got := out.String(); got != "first\nsecond\ntail\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestRedactSubprocessOutputCoversObjectAndWordForms(t *testing.T) {
	cases := []string{
		`{ accessToken: 'eyJabc.def', refreshToken: 'r3fr3sh' }`,
		`AccessToken eyJabc.def expired`,
		`refresh_token=r3fr3sh&uid=u-1`,
		`{"accessToken":"eyJabc.def","Uid":"u-1"}`,
		`session_id: abc123secret`,
	}
	for _, input := range cases {
		got := bridgeproto.RedactText(input)
		for _, secret := range []string{"eyJabc.def", "r3fr3sh", "u-1", "abc123secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("redaction leaked %q from %q: %s", secret, input, got)
			}
		}
	}
	if got := bridgeproto.RedactText(`{"accessToken":"x1"}`); got != `{"accessToken":"[redacted]"}` {
		t.Fatalf("redacted JSON must stay well-formed, got %s", got)
	}
}

func TestLogSubprocessOutputBoundsAndRedactsLongOutput(t *testing.T) {
	var logged bytes.Buffer
	original := trayLog
	trayLog = log.New(&logged, "", 0)
	t.Cleanup(func() { trayLog = original })

	secret := `"accessToken":"` + strings.Repeat("s", 2*maxLoggedSubprocessOutput) + `"`
	logSubprocessOutput("test", []byte(strings.Repeat("x", 300*1024)+" "+secret))
	if logged.Len() > 2*maxLoggedSubprocessOutput {
		t.Fatalf("logged %d bytes, want at most about %d", logged.Len(), maxLoggedSubprocessOutput)
	}

	logged.Reset()
	logSubprocessOutput("test", []byte(strings.Repeat("y", maxLoggedSubprocessOutput-40)+secret))
	if strings.Contains(logged.String(), strings.Repeat("s", 16)) {
		t.Fatalf("a secret cut at the log bound must still be redacted: %q", logged.String()[logged.Len()-80:])
	}
}
