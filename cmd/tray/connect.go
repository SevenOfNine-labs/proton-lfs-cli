package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"proton-lfs-cli/internal/config"
)

const authRateLimitCooldown = time.Hour

// connectToProton runs the tray Connect flow.
// Browser-fork authentication is the only account login path. The tray never
// resolves stored account passwords or invokes direct SRP login.
func connectToProton() {
	if remaining, blocked := activeAuthRateLimit(time.Now()); blocked {
		trayLog.Printf("connect: auth rate limit active; skipping login for %s", formatCooldown(remaining))
		sendNotification("Login rate-limited; wait before retrying")
		return
	}

	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		trayLog.Print("connect: proton-drive-cli binary not found")
		sendNotification("Error: CLI not found")
		return
	}
	trayLog.Printf("connect: using drive-cli at %s", driveCLI)

	prefs := config.LoadPrefs()
	provider := prefs.CredentialProvider
	trayLog.Printf("connect: key-password provider = %s", provider)
	traceID := newAuthTraceID()
	trayLog.Printf("connect: auth trace id = %s", traceID)

	loginArgs, ok := buildTrayLoginArgs(provider)
	if !ok {
		return
	}

	sendNotification("Connecting…")
	go func() {
		out, err := protonDriveLoginWithTrace(driveCLI, provider, traceID, loginArgs...)
		if err != nil {
			trayLog.Printf("connect: login failed: %v", err)
			if isAuthRateLimitedOutput(out) {
				writeAuthRateLimitStatus()
				sendNotification("Login rate-limited by Proton")
			} else {
				sendNotification("Login failed")
			}
			return
		}
		trayLog.Print("connect: login succeeded")
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
		applyAuthReadiness(readiness)
	}()
}

func buildTrayLoginArgs(provider string) ([]string, bool) {
	trayLog.Print("connect: starting browser-fork login")
	return []string{"--key-password-provider", provider}, true
}

func protonDriveLoginWithTrace(driveCLI string, provider string, traceID string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"login"}, args...)
	cmdArgs = append(cmdArgs, "-q")
	trayLog.Printf("connect: exec %s %v", driveCLI, cmdArgs)
	cmd := exec.Command(driveCLI, cmdArgs...)
	cmd.Env = append(cmd.Environ(), withAuthTraceEnv(nil, traceID)...)
	// For pass-cli provider, set PROTON_PASS_CLI_BIN so proton-drive-cli can
	// find pass-cli even when running from a macOS .app bundle with minimal PATH
	if provider == "pass-cli" {
		if passCLI := discoverPassCLIBinary(); passCLI != "" {
			cmd.Env = append(cmd.Env, "PROTON_PASS_CLI_BIN="+passCLI)
			trayLog.Printf("connect: set PROTON_PASS_CLI_BIN=%s", passCLI)
		} else {
			trayLog.Print("connect: warning: pass-cli not found in PATH")
		}
	}
	out, err := runLoggedCommand("connect", cmd)
	if err != nil {
		trayLog.Printf("connect: exec failed: %v\n  output: %s", err, out)
	}
	return out, err
}

func runLoggedCommand(prefix string, cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	var outMu sync.Mutex
	wg.Add(2)
	go streamCommandOutput(prefix, stdout, &out, &outMu, &wg)
	go streamCommandOutput(prefix, stderr, &out, &outMu, &wg)
	wg.Wait()
	err = cmd.Wait()
	return out.Bytes(), err
}

func streamCommandOutput(prefix string, r io.Reader, out *bytes.Buffer, outMu *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		outMu.Lock()
		out.WriteString(line)
		out.WriteByte('\n')
		outMu.Unlock()
		logSubprocessOutput(prefix, []byte(line))
	}
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
