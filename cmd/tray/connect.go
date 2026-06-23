package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"proton-lfs-cli/internal/config"
)

const authRateLimitCooldown = time.Hour

// connectToProton runs the unified tray Connect flow for any credential provider:
//  1. Verify credentials exist via proton-drive-cli credential verify --provider
//  2. If missing and safe for the provider → open interactive credential setup
//  3. If present → log in silently via proton-drive-cli login --credential-provider
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
	trayLog.Printf("connect: credential provider = %s", provider)
	traceID := newAuthTraceID()
	trayLog.Printf("connect: auth trace id = %s", traceID)

	if !credentialVerifyWithTrace(provider, traceID) {
		if !shouldOpenInteractiveCredentialStore(provider) {
			trayLog.Print("connect: pass-cli credentials not found; refusing interactive credential prompt")
			trayLog.Print("connect: create or update a Proton Pass login item with URL https://proton.me")
			sendNotification("Proton Pass item not found")
			return
		}
		trayLog.Print("connect: credentials not found, opening terminal for interactive store")
		// No credentials stored — open terminal for interactive store
		script := fmt.Sprintf("'%s' credential store --provider %s; echo; printf 'Press Enter to close... ' && read", driveCLI, provider)
		cmd := terminalCommand(script)
		if cmd != nil {
			_ = cmd.Start()
		}
		sendNotification("Complete setup in Terminal")
		return
	}

	// Credentials exist — log in silently
	trayLog.Print("connect: credentials verified, starting login")
	sendNotification("Connecting…")
	go func() {
		out, err := protonDriveLoginWithTrace(driveCLI, provider, traceID, "--credential-provider", provider)
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
		sendNotification("Connected to Proton")
		applyConnectStatus(true)
	}()
}

func shouldOpenInteractiveCredentialStore(provider string) bool {
	return provider != config.CredentialProviderPassCLI
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
	out, err := cmd.CombinedOutput()
	logSubprocessOutput("connect", out)
	if err != nil {
		trayLog.Printf("connect: exec failed: %v\n  output: %s", err, out)
	}
	return out, err
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
