package main

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"proton-lfs-cli/internal/config"
)

// Function vars for testability — tests swap these to inject mocks.
var (
	findDriveCLI = discoverDriveCLIBinary
	findAdapter  = discoverAdapterBinary
	loginDrive   = cliDriveLogin
)

// cliDriveLogin runs proton-drive-cli login without -q, capturing stderr
// in the returned error so CLI users see the actual failure reason.
func cliDriveLogin(driveCLI string, args ...string) error {
	cmdArgs := append([]string{"login"}, args...)
	var stderr bytes.Buffer
	cmd := exec.Command(driveCLI, cmdArgs...)
	cmd.Stderr = &stderr
	// For pass-cli provider, set PROTON_PASS_CLI_BIN so proton-drive-cli can
	// find pass-cli even when running from a macOS .app bundle with minimal PATH.
	provider := extractKeyPasswordProviderFromArgs(args)
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

// extractKeyPasswordProviderFromArgs scans args for --key-password-provider
// and returns the value, or empty string if not found.
func extractKeyPasswordProviderFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--key-password-provider" && i+1 < len(args) {
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
		_, _ = fmt.Fprintf(w, "Transfer: %s\n", transferStatusText(report))
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

// cliConfig shows or sets the browser-fork key-password provider.
func cliConfig(w io.Writer, args []string) int {
	if len(args) == 0 {
		prefs := config.LoadPrefs()
		_, _ = fmt.Fprintln(w, prefs.CredentialProvider)
		return 0
	}

	provider := args[0]
	switch provider {
	case "--help", "-h":
		_, _ = fmt.Fprintln(w, "Usage: proton-lfs-cli config [provider]")
		_, _ = fmt.Fprintf(w, "\nShow or set the browser-fork key-password provider.\n")
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
	_, _ = fmt.Fprintf(w, "Key-password provider set to %s\n", provider)
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
	args := buildProtonTransferArgs(prefs.CredentialProvider, driveCLIPath)
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

// cliLogin handles browser-fork login for the configured key-password provider.
func cliLogin(w io.Writer) int {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		_, _ = fmt.Fprintln(w, "error: proton-drive-cli not found")
		return 1
	}

	prefs := config.LoadPrefs()
	provider := prefs.CredentialProvider

	_, _ = fmt.Fprintln(w, "Logging in...")
	if err := loginDrive(driveCLI, "--key-password-provider", provider); err != nil {
		_, _ = fmt.Fprintf(w, "error: login failed: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(w, "Connected to Proton")
	return 0
}
