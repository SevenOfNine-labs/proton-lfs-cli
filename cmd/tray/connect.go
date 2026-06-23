package main

import (
	"fmt"
	"os/exec"

	"proton-lfs-cli/internal/config"
)

// connectToProton runs the unified tray Connect flow for any credential provider:
//  1. Verify credentials exist via proton-drive-cli credential verify --provider
//  2. If missing and safe for the provider → open interactive credential setup
//  3. If present → log in silently via proton-drive-cli login --credential-provider
func connectToProton() {
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
		if err := protonDriveLoginWithTrace(driveCLI, provider, traceID, "--credential-provider", provider); err != nil {
			trayLog.Printf("connect: login failed: %v", err)
			sendNotification("Login failed")
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

func protonDriveLoginWithTrace(driveCLI string, provider string, traceID string, args ...string) error {
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
	return err
}
