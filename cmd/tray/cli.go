package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

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
	cmd.Env = driveCLIEnv(cmd.Environ())
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return err
	}
	return nil
}

// cliStatus prints session, LFS, credential store, and transfer status.
func cliStatus(w io.Writer) int {
	switch meta, ok := readSessionMetadata(); {
	case ok && validSessionShape(meta):
		_, _ = fmt.Fprintf(w, "Session:  signed in; credential store %s (%s); proton-drive-cli doctor verifies it\n", meta.CredentialStore, meta.CredentialHost)
	case isSessionActive():
		_, _ = fmt.Fprintln(w, "Session:  invalid session file; run proton-drive-cli login")
	default:
		_, _ = fmt.Fprintln(w, "Session:  not connected")
	}

	if isLFSEnabled() {
		_, _ = fmt.Fprintln(w, "LFS:      enabled")
	} else {
		_, _ = fmt.Fprintln(w, "LFS:      not registered")
	}

	prefs := config.LoadPrefs()
	_, _ = fmt.Fprintf(w, "Store:    %s (next login)\n", prefs.CredentialStore)

	report, err := config.ReadStatus()
	if err != nil {
		_, _ = fmt.Fprintln(w, "Transfer: no data")
	} else {
		_, _ = fmt.Fprintf(w, "Transfer: %s\n", transferStatusText(report))
	}
	readiness := inspectAuthReadiness(timeNow())
	_, _ = fmt.Fprintf(w, "Readiness: %s\n", strings.TrimPrefix(readiness.statusTitle, "Status: "))
	_, _ = fmt.Fprintf(w, "Refresh:  %s\n", strings.TrimPrefix(readiness.refreshTitle, "Refresh: "))
	return 0
}

// cliLogout delegates to proton-drive-cli logout to clear the session.
func cliLogout(w io.Writer) int {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		_, _ = fmt.Fprintln(w, "error: proton-drive-cli not found")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), driveCLITimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := driveCLICommand(ctx, driveCLI, "logout")
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

// cliConfig shows or sets the credential store the next login saves the
// session tokens and key password in.
func cliConfig(w io.Writer, args []string) int {
	if len(args) == 0 {
		prefs := config.LoadPrefs()
		_, _ = fmt.Fprintln(w, prefs.CredentialStore)
		return 0
	}

	store := args[0]
	switch store {
	case "--help", "-h":
		_, _ = fmt.Fprintln(w, "Usage: proton-lfs-cli config [store]")
		_, _ = fmt.Fprintf(w, "\nShow or set the credential store the next login saves the session tokens and key password in.\n")
		_, _ = fmt.Fprintf(w, "With no argument, prints the current store.\n\n")
		_, _ = fmt.Fprintf(w, "Stores: %s, %s\n",
			config.CredentialStoreGitCredential, config.CredentialStorePassCLI)
		return 0
	case config.CredentialStoreGitCredential, config.CredentialStorePassCLI:
	default:
		_, _ = fmt.Fprintf(w, "unknown credential store: %s\n", store)
		_, _ = fmt.Fprintf(w, "valid stores: %s, %s\n",
			config.CredentialStoreGitCredential, config.CredentialStorePassCLI)
		return 1
	}

	prefs := config.LoadPrefs()
	prefs.CredentialStore = store
	if err := config.SavePrefs(prefs); err != nil {
		_, _ = fmt.Fprintf(w, "error saving config: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(w, "Credential store set to %s\n", store)
	return 0
}

// cliRegister enables the Proton LFS backend in git global config. It writes
// the same settings as the tray's "Enable LFS Backend" item.
func cliRegister(w io.Writer) int {
	adapterPath := findAdapter()
	if adapterPath == "" {
		_, _ = fmt.Fprintln(w, "error: adapter binary not found")
		return 1
	}

	prefs := config.LoadPrefs()
	driveCLIPath := findDriveCLI()
	if err := writeGitLFSRegistration(adapterPath, buildProtonTransferArgs(driveCLIPath)); err != nil {
		_, _ = fmt.Fprintf(w, "error: git config failed: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintln(w, "LFS backend enabled")
	_, _ = fmt.Fprintf(w, "  adapter: %s\n", adapterPath)
	if driveCLIPath != "" {
		_, _ = fmt.Fprintf(w, "  drive-cli: %s\n", driveCLIPath)
	}
	_, _ = fmt.Fprintf(w, "  store: %s\n", prefs.CredentialStore)
	return 0
}

// cliLogin handles browser-fork login with the configured credential store.
func cliLogin(w io.Writer) int {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		_, _ = fmt.Fprintln(w, "error: proton-drive-cli not found")
		return 1
	}

	prefs := config.LoadPrefs()
	_, _ = fmt.Fprintln(w, "Logging in...")
	if err := loginDrive(driveCLI, "--credential-store", prefs.CredentialStore); err != nil {
		_, _ = fmt.Fprintf(w, "error: login failed: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(w, "Connected to Proton")
	return 0
}
