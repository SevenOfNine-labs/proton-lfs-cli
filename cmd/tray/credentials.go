package main

import (
	"os/exec"
)

// credentialVerify checks whether credentials exist for the given provider
// by running proton-drive-cli credential verify --provider <provider>.
// GIT_TERMINAL_PROMPT=0 and GCM_INTERACTIVE=never suppress interactive
// prompts so the check is truly silent.
func credentialVerify(provider string) bool {
	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		trayLog.Print("credential-verify: proton-drive-cli not found")
		return false
	}
	args := []string{"credential", "verify", "--provider", provider, "-q"}
	trayLog.Printf("credential-verify: exec %s %v", driveCLI, args)
	cmd := exec.Command(driveCLI, args...)
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
	}
	// For pass-cli provider, set PROTON_PASS_CLI_BIN so proton-drive-cli can
	// find pass-cli even when running from a macOS .app bundle with minimal PATH
	if provider == "pass-cli" {
		if passCLI := discoverPassCLIBinary(); passCLI != "" {
			env = append(env, "PROTON_PASS_CLI_BIN="+passCLI)
			trayLog.Printf("credential-verify: set PROTON_PASS_CLI_BIN=%s", passCLI)
		} else {
			trayLog.Print("credential-verify: warning: pass-cli not found in PATH")
		}
	}
	cmd.Env = append(cmd.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trayLog.Printf("credential-verify: failed: %v\n  output: %s", err, out)
		return false
	}
	trayLog.Print("credential-verify: ok")
	return true
}
