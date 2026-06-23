package main

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"fyne.io/systray"

	"proton-lfs-cli/internal/config"
)

//go:embed icons/*.png
var iconFS embed.FS

var (
	iconIdle    []byte
	iconOK      []byte
	iconError   []byte
	iconSyncing []byte
)

// Menu items that get updated dynamically.
var (
	mStatus       *systray.MenuItem
	mSession      *systray.MenuItem
	mTransfers    *systray.MenuItem
	mRefresh      *systray.MenuItem
	mPrimaryAuth  *systray.MenuItem
	mDataPassword *systray.MenuItem
	mRecheck      *systray.MenuItem
	mCredGit      *systray.MenuItem
	mCredPass     *systray.MenuItem
	mRegister     *systray.MenuItem
	mOpenLogs     *systray.MenuItem
	mDoctor       *systray.MenuItem

	currentAuthAction authMenuAction
)

type authMenuAction string

const (
	authActionConnect    authMenuAction = "connect"
	authActionDisconnect authMenuAction = "disconnect"
	authActionNone       authMenuAction = "none"
)

func init() {
	iconIdle, _ = iconFS.ReadFile("icons/icon_idle.png")
	iconOK, _ = iconFS.ReadFile("icons/icon_ok.png")
	iconError, _ = iconFS.ReadFile("icons/icon_error.png")
	iconSyncing, _ = iconFS.ReadFile("icons/icon_syncing.png")
}

func setupMenu() {
	systray.SetIcon(iconIdle)
	systray.SetTemplateIcon(iconIdle, iconIdle)
	systray.SetTooltip("Proton LFS")

	mVersion := systray.AddMenuItem(fmt.Sprintf("Proton LFS %s", Version), "")
	mVersion.Disable()

	systray.AddSeparator()

	mStatus = systray.AddMenuItem("Status: Checking...", "Current Proton LFS readiness")
	mSession = systray.AddMenuItem("Session: Checking...", "Current Proton session state")
	mTransfers = systray.AddMenuItem("Transfers: Checking...", "Current Git LFS transfer readiness")
	mRefresh = systray.AddMenuItem("Refresh: Checking...", "Current session refresh health")
	mStatus.Disable()
	mSession.Disable()
	mTransfers.Disable()
	mRefresh.Disable()

	systray.AddSeparator()

	mPrimaryAuth = systray.AddMenuItem("Connect to Proton...", "Start browser sign-in")
	mDataPassword = systray.AddMenuItem("Configure Data Password...", "Open mailbox/data password setup instructions")
	mRecheck = systray.AddMenuItem("Recheck Status", "Refresh tray readiness state")

	systray.AddSeparator()

	mCredPass = systray.AddMenuItemCheckbox("Use Proton Pass", "Store browser-fork key password in Proton Pass CLI", false)
	mCredGit = systray.AddMenuItemCheckbox("Use Git Credential Manager", "Store browser-fork key password in the OS credential manager", false)

	prefs := config.LoadPrefs()
	applyCredCheckmarks(prefs.CredentialProvider)

	systray.AddSeparator()

	mRegister = systray.AddMenuItemCheckbox("Enable LFS Backend", "Configure Git to route LFS transfers through Proton Drive", false)

	systray.AddSeparator()

	mOpenLogs = systray.AddMenuItem("Open Logs", "Open a terminal tailing Proton LFS tray logs")
	mDoctor = systray.AddMenuItem("Run Doctor...", "Open offline Proton Drive doctor checks")
	mAutoStart := systray.AddMenuItemCheckbox("Start at System Login", "Automatically launch the tray app when you log in to your computer", isAutoStartEnabled())

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit Proton LFS", "Quit Proton LFS tray")

	// Event loop
	go func() {
		for {
			select {
			case <-mCredGit.ClickedCh:
				switchCredentialProvider(config.CredentialProviderGitCredential)
			case <-mCredPass.ClickedCh:
				switchCredentialProvider(config.CredentialProviderPassCLI)
			case <-mPrimaryAuth.ClickedCh:
				handlePrimaryAuthAction()
			case <-mDataPassword.ClickedCh:
				openDataPasswordGuide()
			case <-mRecheck.ClickedCh:
				recheckStatus()
			case <-mRegister.ClickedCh:
				registerGitLFS()
			case <-mOpenLogs.ClickedCh:
				openLogs()
			case <-mDoctor.ClickedCh:
				runDoctor()
			case <-mAutoStart.ClickedCh:
				toggleAutoStart(mAutoStart)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func handlePrimaryAuthAction() {
	switch currentAuthAction {
	case authActionConnect:
		connectToProton()
	case authActionDisconnect:
		disconnectFromProton()
	}
}

// applyRegisterStatus updates the Register menu item checkmark and title.
func applyRegisterStatus(enabled bool) {
	if mRegister == nil {
		return
	}
	if enabled {
		mRegister.SetTitle("LFS Backend Enabled")
		mRegister.Check()
	} else {
		mRegister.SetTitle("Enable LFS Backend")
		mRegister.Uncheck()
	}
}

func applyCredCheckmarks(provider string) {
	if mCredGit == nil || mCredPass == nil {
		return
	}
	if provider == config.CredentialProviderGitCredential {
		mCredGit.Check()
		mCredPass.Uncheck()
	} else {
		mCredGit.Uncheck()
		mCredPass.Check()
	}
}

func switchCredentialProvider(provider string) {
	prefs := config.LoadPrefs()
	prefs.CredentialProvider = provider
	_ = config.SavePrefs(prefs)
	applyCredCheckmarks(provider)
}

func registerGitLFS() {
	adapterPath := discoverAdapterBinary()
	if adapterPath == "" {
		sendNotification("Error: adapter binary not found")
		return
	}
	if err := exec.Command("git", "config", "--global", "lfs.customtransfer.proton.path", adapterPath).Run(); err != nil {
		sendNotification("Error: git config failed")
		return
	}

	prefs := config.LoadPrefs()
	driveCLIPath := discoverDriveCLIBinary()
	args := buildProtonTransferArgsFromPrefs(prefs, driveCLIPath)
	if err := exec.Command("git", "config", "--global", "lfs.customtransfer.proton.args", args).Run(); err != nil {
		sendNotification("Error: git config failed")
		return
	}
	if err := exec.Command("git", "config", "--global", "lfs.standalonetransferagent", "proton").Run(); err != nil {
		sendNotification("Error: git config failed")
		return
	}

	applyRegisterStatus(true)
	sendNotification("LFS Backend Enabled")
}

// isLFSEnabled checks whether the Proton LFS adapter is registered in git global config.
func isLFSEnabled() bool {
	out, err := exec.Command("git", "config", "--global", "lfs.standalonetransferagent").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "proton"
}

// isSessionActive checks whether a proton-drive-cli session file exists.
func isSessionActive() bool {
	sf := sessionFilePath()
	if sf == "" {
		return false
	}
	_, err := os.Stat(sf)
	return err == nil
}

func disconnectFromProton() {
	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		trayLog.Print("disconnect: proton-drive-cli binary not found")
		sendNotification("Error: CLI not found")
		return
	}
	go func() {
		cmd := exec.Command(driveCLI, "logout")
		out, err := cmd.CombinedOutput()
		logSubprocessOutput("disconnect", out)
		if err != nil {
			trayLog.Printf("disconnect: logout failed: %v", err)
			sendNotification("Disconnect failed")
			return
		}
		trayLog.Print("disconnect: logout succeeded")
		sendNotification("Disconnected from Proton")
		applyLoginStatus()
	}()
}

func recheckStatus() {
	applyStatus()
	applyLoginStatus()
	applyLFSStatus()
	maybeRefreshSession()
}

func openLogs() {
	logPath := trayLogPath()
	cmd := terminalCommand(fmt.Sprintf("tail -n 200 -f %s", shellQuoteArg(logPath)))
	if cmd == nil || cmd.Start() != nil {
		sendNotification("Logs: " + logPath)
	}
}

func runDoctor() {
	driveCLI := discoverDriveCLIBinary()
	if driveCLI == "" {
		sendNotification("Error: CLI not found")
		return
	}
	prefs := config.LoadPrefs()
	args := []string{
		shellQuoteArg(driveCLI),
		"doctor",
		"--key-password-provider",
		shellQuoteArg(prefs.CredentialProvider),
	}
	if prefs.DataCredentialProvider != "" {
		args = append(args,
			"--data-credential-provider",
			shellQuoteArg(prefs.DataCredentialProvider),
			"--data-credential-host",
			shellQuoteArg(resolveDataCredentialHost(prefs)),
		)
	}
	cmd := terminalCommand(strings.Join(args, " ") + "; echo; echo 'Press Enter to close...'; read -r")
	if cmd == nil || cmd.Start() != nil {
		sendNotification("Doctor could not open terminal")
	}
}

func openDataPasswordGuide() {
	host := config.DefaultDataCredentialHost
	script := strings.Join([]string{
		"echo 'Proton LFS data-password setup'",
		"echo",
		fmt.Sprintf("echo 'Store the Proton mailbox/data password under https://%s.'", host),
		"echo 'Then set PROTON_DATA_CREDENTIAL_PROVIDER or update Git LFS args with --data-credential-provider.'",
		"echo",
		"echo 'Recommended provider: pass-cli'",
		"echo",
		"echo 'Press Enter to close...'",
		"read -r",
	}, "; ")
	cmd := terminalCommand(script)
	if cmd == nil || cmd.Start() != nil {
		sendNotification("Configure data password in your credential provider")
	}
}

func resolveDataCredentialHost(prefs config.Preferences) string {
	if strings.TrimSpace(prefs.DataCredentialHost) != "" {
		return strings.TrimSpace(prefs.DataCredentialHost)
	}
	return config.DefaultDataCredentialHost
}

// sendNotification shows a native macOS notification banner, or falls back
// to notify-send on Linux.
func sendNotification(msg string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "Proton LFS"`, msg)).Start()
	case "linux":
		_ = exec.Command("notify-send", "Proton LFS", msg).Start()
	}
}

func toggleAutoStart(item *systray.MenuItem) {
	if item.Checked() {
		if err := setAutoStart(false); err == nil {
			item.Uncheck()
		}
	} else {
		if err := setAutoStart(true); err == nil {
			item.Check()
		}
	}
}

// terminalCommand returns an exec.Cmd that opens a terminal and runs the given shell snippet.
func terminalCommand(script string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return terminalCommandDarwin(script)
	case "linux":
		// Try common terminal emulators
		for _, term := range []string{"x-terminal-emulator", "gnome-terminal", "xterm"} {
			if p, err := exec.LookPath(term); err == nil {
				return exec.Command(p, "-e", "bash", "-c", script)
			}
		}
		return nil
	case "windows":
		return exec.Command("cmd", "/c", "start", "cmd", "/k", script)
	default:
		return nil
	}
}

// terminalCommandDarwin writes the script to a temp file and tells Terminal
// to execute it. This avoids the raw command being echoed in the terminal.
func terminalCommandDarwin(script string) *exec.Cmd {
	f, err := os.CreateTemp("", "proton-lfs-*.sh")
	if err != nil {
		return nil
	}
	content := "#!/bin/zsh\nclear\n" + script + "\nrm -f \"$0\"\n"
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return nil
	}
	_ = f.Close()
	_ = os.Chmod(f.Name(), 0o700)
	return exec.Command("osascript", "-e",
		fmt.Sprintf(`tell application "Terminal" to do script "%s"`, f.Name()))
}
