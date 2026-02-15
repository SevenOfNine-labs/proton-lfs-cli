package main

import (
	"fmt"
	"os/exec"

	"proton-git-lfs/internal/config"
)

// connectToProton runs the unified tray Connect flow for any credential provider:
//  1. Verify credentials exist via proton-drive-cli credential verify --provider
//  2. If missing → open terminal for interactive credential store
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

	if !credentialVerify(provider) {
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
		if err := protonDriveLogin(driveCLI, "--credential-provider", provider); err != nil {
			trayLog.Printf("connect: login failed: %v", err)
			sendNotification("Login failed")
			return
		}
		trayLog.Print("connect: login succeeded")
		sendNotification("Connected to Proton")
		applyConnectStatus(true)
	}()
}

// protonDriveLogin runs proton-drive-cli login with the given extra args.
func protonDriveLogin(driveCLI string, args ...string) error {
	cmdArgs := append([]string{"login"}, args...)
	cmdArgs = append(cmdArgs, "-q")
	trayLog.Printf("connect: exec %s %v", driveCLI, cmdArgs)
	cmd := exec.Command(driveCLI, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trayLog.Printf("connect: exec failed: %v\n  output: %s", err, out)
	}
	return err
}
