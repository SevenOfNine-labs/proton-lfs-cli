package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"proton-lfs-cli/internal/config"
)

// Function vars for testability — tests swap these to inject mocks.
var (
	findDriveCLI     = discoverDriveCLIBinary
	findAdapter      = discoverAdapterBinary
	verifyCredential = credentialVerify
	loginDrive       = cliDriveLogin
)

// cliDriveLogin runs proton-drive-cli login without -q, capturing stderr
// in the returned error so CLI users see the actual failure reason.
func cliDriveLogin(driveCLI string, args ...string) error {
	cmdArgs := append([]string{"login"}, args...)
	var stderr bytes.Buffer
	cmd := exec.Command(driveCLI, cmdArgs...)
	cmd.Stderr = &stderr
	// For pass-cli provider, set PROTON_PASS_CLI_BIN so proton-drive-cli can
	// find pass-cli even when running from a macOS .app bundle with minimal PATH
	// Extract provider from args (looks for "--credential-provider" or "--provider")
	provider := extractProviderFromArgs(args)
	if provider == "pass-cli" {
		if passCLI := discoverPassCLIBinary(); passCLI != "" {
			cmd.Env = append(cmd.Environ(), "PROTON_PASS_CLI_BIN="+passCLI)
		}
	}
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

// extractProviderFromArgs scans args for --credential-provider or --provider
// and returns the value, or empty string if not found.
func extractProviderFromArgs(args []string) string {
	for i, arg := range args {
		if (arg == "--credential-provider" || arg == "--provider") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// cliStatus prints session, LFS, provider, and transfer status.
func cliStatus(w io.Writer) int {
	if isSessionActive() {
		_, _ = fmt.Fprintln(w, "Session:  logged in")
	} else {
		_, _ = fmt.Fprintln(w, "Session:  not connected")
	}

	if isLFSEnabled() {
		_, _ = fmt.Fprintln(w, "LFS:      enabled")
	} else {
		_, _ = fmt.Fprintln(w, "LFS:      not registered")
	}

	prefs := config.LoadPrefs()
	_, _ = fmt.Fprintf(w, "Provider: %s\n", prefs.CredentialProvider)

	report, err := config.ReadStatus()
	if err != nil {
		_, _ = fmt.Fprintln(w, "Transfer: no data")
	} else {
		switch report.State {
		case config.StateTransferring:
			_, _ = fmt.Fprintf(w, "Transfer: %s in progress\n", report.LastOp)
		case config.StateError:
			msg := "failed"
			if report.Error != "" {
				msg = report.Error
			}
			_, _ = fmt.Fprintf(w, "Transfer: %s %s (%s)\n", report.LastOp, relativeTime(report.Timestamp), msg)
		case config.StateOK:
			_, _ = fmt.Fprintf(w, "Transfer: %s %s (ok)\n", report.LastOp, relativeTime(report.Timestamp))
		default:
			_, _ = fmt.Fprintln(w, "Transfer: idle")
		}
	}
	return 0
}

// cliLogout delegates to proton-drive-cli logout to clear the session.
func cliLogout(w io.Writer) int {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		_, _ = fmt.Fprintln(w, "error: proton-drive-cli not found")
		return 1
	}

	var stderr bytes.Buffer
	cmd := exec.Command(driveCLI, "logout")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = fmt.Fprintf(w, "error: logout failed: %s\n", strings.TrimSpace(stderr.String()))
		} else {
			_, _ = fmt.Fprintf(w, "error: logout failed: %v\n", err)
		}
		return 1
	}
	_, _ = fmt.Fprintln(w, "Logged out")
	return 0
}

// cliConfig shows or sets the credential provider.
func cliConfig(w io.Writer, args []string) int {
	if len(args) == 0 {
		prefs := config.LoadPrefs()
		_, _ = fmt.Fprintln(w, prefs.CredentialProvider)
		return 0
	}

	provider := args[0]
	switch provider {
	case "--help", "-h":
		_, _ = fmt.Fprintln(w, "Usage: proton-git-lfs config [provider]")
		_, _ = fmt.Fprintf(w, "\nShow or set the credential provider.\n")
		_, _ = fmt.Fprintf(w, "With no argument, prints the current provider.\n\n")
		_, _ = fmt.Fprintf(w, "Providers: %s, %s\n",
			config.CredentialProviderGitCredential, config.CredentialProviderPassCLI)
		return 0
	case config.CredentialProviderGitCredential, config.CredentialProviderPassCLI:
		// valid
	default:
		_, _ = fmt.Fprintf(w, "unknown provider: %s\n", provider)
		_, _ = fmt.Fprintf(w, "valid providers: %s, %s\n",
			config.CredentialProviderGitCredential, config.CredentialProviderPassCLI)
		return 1
	}

	prefs := config.LoadPrefs()
	prefs.CredentialProvider = provider
	if err := config.SavePrefs(prefs); err != nil {
		_, _ = fmt.Fprintf(w, "error saving config: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(w, "Credential provider set to %s\n", provider)
	return 0
}

// cliRegister enables the Proton LFS backend in git global config.
func cliRegister(w io.Writer) int {
	adapterPath := findAdapter()
	if adapterPath == "" {
		_, _ = fmt.Fprintln(w, "error: adapter binary not found")
		return 1
	}

	if err := exec.Command("git", "config", "--global",
		"lfs.customtransfer.proton.path", adapterPath).Run(); err != nil {
		_, _ = fmt.Fprintf(w, "error: git config failed: %v\n", err)
		return 1
	}

	prefs := config.LoadPrefs()
	driveCLIPath := findDriveCLI()
	args := "--backend sdk"
	if prefs.CredentialProvider == config.CredentialProviderGitCredential {
		args += " --credential-provider git-credential"
	}
	if driveCLIPath != "" {
		args += " --drive-cli-bin " + driveCLIPath
	}
	if err := exec.Command("git", "config", "--global",
		"lfs.customtransfer.proton.args", args).Run(); err != nil {
		_, _ = fmt.Fprintf(w, "error: git config failed: %v\n", err)
		return 1
	}
	if err := exec.Command("git", "config", "--global",
		"lfs.standalonetransferagent", "proton").Run(); err != nil {
		_, _ = fmt.Fprintf(w, "error: git config failed: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintln(w, "LFS backend enabled")
	_, _ = fmt.Fprintf(w, "  adapter: %s\n", adapterPath)
	if driveCLIPath != "" {
		_, _ = fmt.Fprintf(w, "  drive-cli: %s\n", driveCLIPath)
	}
	_, _ = fmt.Fprintf(w, "  provider: %s\n", prefs.CredentialProvider)
	return 0
}

// cliLogin handles the unified login flow for any credential provider.
// 1. Verify credentials exist via proton-drive-cli credential verify --provider
// 2. If missing, start interactive credential store
// 3. Login via proton-drive-cli login --credential-provider
func cliLogin(w io.Writer) int {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		_, _ = fmt.Fprintln(w, "error: proton-drive-cli not found")
		return 1
	}

	prefs := config.LoadPrefs()
	provider := prefs.CredentialProvider

	if !verifyCredential(provider) {
		_, _ = fmt.Fprintln(w, "No credentials stored. Starting credential setup...")
		cmd := exec.Command(driveCLI, "credential", "store", "--provider", provider)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// For pass-cli provider, set PROTON_PASS_CLI_BIN
		if provider == "pass-cli" {
			if passCLI := discoverPassCLIBinary(); passCLI != "" {
				cmd.Env = append(cmd.Environ(), "PROTON_PASS_CLI_BIN="+passCLI)
			}
		}
		if err := cmd.Run(); err != nil {
			_, _ = fmt.Fprintf(w, "error: credential store failed: %v\n", err)
			return 1
		}
		if !verifyCredential(provider) {
			_, _ = fmt.Fprintln(w, "error: credentials not found after store")
			return 1
		}
	}

	_, _ = fmt.Fprintln(w, "Logging in...")
	if err := loginDrive(driveCLI, "--credential-provider", provider); err != nil {
		_, _ = fmt.Fprintf(w, "error: login failed: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(w, "Connected to Proton")
	return 0
}
