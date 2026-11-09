package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

const authRateLimitCooldown = time.Hour

// subprocessWaitDelay bounds exec.Cmd.Wait after the child has exited or its
// context expired: a grandchild that inherited the output pipes (for example
// a browser spawned by proton-drive-cli login) must not keep the tray waiting.
const subprocessWaitDelay = 5 * time.Second

// connectToProton runs `proton-drive-cli login`, the browser-fork sign-in,
// with the configured credential store.
func connectToProton() {
	if remaining, blocked := activeAuthRateLimit(time.Now()); blocked {
		trayLog.Printf("connect: auth rate limit active; skipping login for %s", formatCooldown(remaining))
		sendNotification("Login rate-limited; wait before retrying")
		return
	}

	driveCLI := findDriveCLI()
	if driveCLI == "" {
		trayLog.Print("connect: proton-drive-cli binary not found")
		sendNotification("Error: CLI not found")
		return
	}
	trayLog.Printf("connect: using drive-cli at %s", driveCLI)

	prefs := config.LoadPrefs()
	store := prefs.CredentialStore
	trayLog.Printf("connect: credential store = %s", store)
	traceID := newAuthTraceID()
	trayLog.Printf("connect: auth trace id = %s", traceID)

	sendNotification("Connecting…")
	go func() {
		out, err := protonDriveLoginWithTrace(driveCLI, store, traceID, "--credential-store", store)
		if err != nil {
			trayLog.Printf("connect: login failed: %v", err)
			if isAuthRateLimitedOutput(out) {
				writeAuthRateLimitStatus()
				sendNotification("Login rate-limited by Proton")
			} else if isBrowserNotOpenedOutput(out) {
				sendNotification("No browser opened; run proton-lfs-cli login in a terminal")
			} else {
				sendNotification("Login failed")
			}
			return
		}
		trayLog.Print("connect: login succeeded")
		clearRefreshState()
		readiness := inspectAuthReadiness(timeNow())
		trayLog.Printf("connect: post-login readiness: %s; %s",
			readiness.statusTitle, readiness.transferTitle)
		if readiness.ready {
			sendNotification("Proton LFS Ready")
		} else if readiness.blocked {
			sendNotification("Signed in; setup needed")
		} else {
			sendNotification("Signed in")
		}
		refreshTrayState()
	}()
}

func protonDriveLoginWithTrace(driveCLI string, store string, traceID string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"login"}, args...)
	cmdArgs = append(cmdArgs, "-q")
	trayLog.Printf("connect: exec %s %v", driveCLI, cmdArgs)
	cmd := exec.Command(driveCLI, cmdArgs...)
	cmd.Env = driveCLIEnv(append(cmd.Environ(), withAuthTraceEnv(nil, traceID)...))
	if store == config.CredentialStorePassCLI && discoverPassCLIBinary() == "" {
		trayLog.Print("connect: warning: pass-cli not found in PATH")
	}
	out, err := runLoggedCommand("connect", cmd)
	if err != nil {
		trayLog.Printf("connect: exec failed: %v", err)
	}
	return out, err
}

// runLoggedCommand runs cmd, logs every output line (redacted) under prefix,
// and returns the combined stdout and stderr. lineLogger has no line-length
// limit, unlike bufio.Scanner (bufio.MaxScanTokenSize).
func runLoggedCommand(prefix string, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	var outMu sync.Mutex
	stdout := newLineLogger(prefix, &out, &outMu)
	stderr := newLineLogger(prefix, &out, &outMu)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = subprocessWaitDelay
	err := cmd.Run()
	stdout.Flush()
	stderr.Flush()
	return out.Bytes(), err
}

// lineLogger is an io.Writer that forwards each complete line to the tray
// log and to a shared output buffer. One lineLogger serves one stream; the
// buffer is shared between streams and guarded by outMu.
type lineLogger struct {
	prefix  string
	out     *bytes.Buffer
	outMu   *sync.Mutex
	pending bytes.Buffer
}

func newLineLogger(prefix string, out *bytes.Buffer, outMu *sync.Mutex) *lineLogger {
	return &lineLogger{prefix: prefix, out: out, outMu: outMu}
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.pending.Write(p)
	for {
		idx := bytes.IndexByte(l.pending.Bytes(), '\n')
		if idx < 0 {
			return len(p), nil
		}
		line := make([]byte, idx)
		copy(line, l.pending.Next(idx+1))
		l.emit(line)
	}
}

// Flush emits a trailing line that was not newline-terminated.
func (l *lineLogger) Flush() {
	if l.pending.Len() == 0 {
		return
	}
	line := make([]byte, l.pending.Len())
	copy(line, l.pending.Next(l.pending.Len()))
	l.emit(line)
}

func (l *lineLogger) emit(line []byte) {
	l.outMu.Lock()
	l.out.Write(line)
	l.out.WriteByte('\n')
	l.outMu.Unlock()
	logSubprocessOutput(l.prefix, line)
}

func activeAuthRateLimit(now time.Time) (time.Duration, bool) {
	report, err := config.ReadStatus()
	if err != nil || report.State != config.StateRateLimited || report.LastOp != "login" {
		return 0, false
	}
	if report.ErrorCode != "RATE_LIMITED" && report.ErrorCode != "rate_limited" {
		return 0, false
	}
	if report.Timestamp.IsZero() {
		return authRateLimitCooldown, true
	}
	remaining := report.Timestamp.Add(authRateLimitCooldown).Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

func isAuthRateLimitedOutput(out []byte) bool {
	text := string(out)
	return strings.Contains(text, `"errorCode":"RATE_LIMITED"`) ||
		strings.Contains(text, `"clientCode":"RATE_LIMITED"`) ||
		strings.Contains(text, `"protonCode":2028`) ||
		strings.Contains(text, "Proton code 2028")
}

// isBrowserNotOpenedOutput reports the login failure proton-drive-cli
// raises under -q when no browser opened (src/cli/login.ts BROWSER_NOT_OPENED).
func isBrowserNotOpenedOutput(out []byte) bool {
	return strings.Contains(string(out), `"reason":"browser-not-opened"`)
}

func writeAuthRateLimitStatus() {
	detail := "Proton blocked the sign-in attempt. Wait before retrying; avoid VPN/shared exit IPs for the next canary."
	if err := config.WriteStatus(config.StatusReport{
		State:       config.StateRateLimited,
		LastOp:      "login",
		Error:       "Proton login rate-limited",
		ErrorCode:   "RATE_LIMITED",
		ErrorDetail: detail,
		Retryable:   true,
		Temporary:   true,
		Timestamp:   time.Now(),
	}); err != nil {
		trayLog.Printf("connect: failed to write rate-limit status: %v", err)
	}
}

func formatCooldown(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute).Minutes()))
}
