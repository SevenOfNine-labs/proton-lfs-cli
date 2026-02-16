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
	mCredGit  *systray.MenuItem
	mCredPass *systray.MenuItem
	mConnect  *systray.MenuItem
	mRegister *systray.MenuItem
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

	mCredMenu := systray.AddMenuItem("Credential Store", "Choose where your Proton credentials are stored")
	mCredGit = mCredMenu.AddSubMenuItem("Git Credential Manager", "macOS Keychain, Windows Credential Manager, or Linux Secret Service")
	mCredPass = mCredMenu.AddSubMenuItem("Proton Pass", "Proton Pass CLI (pass-cli) for encrypted credential storage")

	prefs := config.LoadPrefs()
	applyCredCheckmarks(prefs.CredentialProvider)

	systray.AddSeparator()

	mConnect = systray.AddMenuItemCheckbox("Connect to Proton\u2026", "Store credentials and authenticate with Proton", false)
	mRegister = systray.AddMenuItemCheckbox("Enable LFS Backend", "Configure Git to route LFS transfers through Proton Drive", false)

	systray.AddSeparator()

	mAutoStart := systray.AddMenuItemCheckbox("Start at System Login", "Automatically launch the tray app when you log in to your computer", isAutoStartEnabled())

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Proton LFS tray")

	// Event loop
	go func() {
		for {
			select {
			case <-mCredGit.ClickedCh:
				switchCredentialProvider(config.CredentialProviderGitCredential)
			case <-mCredPass.ClickedCh:
				switchCredentialProvider(config.CredentialProviderPassCLI)
			case <-mConnect.ClickedCh:
				connectToProton()
			case <-mRegister.ClickedCh:
				registerGitLFS()
			case <-mAutoStart.ClickedCh:
				toggleAutoStart(mAutoStart)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// applyConnectStatus updates the Connect menu item checkmark and title.
func applyConnectStatus(connected bool) {
	if connected {
		mConnect.SetTitle("Connected to Proton")
		mConnect.Check()
	} else {
		mConnect.SetTitle("Connect to Proton\u2026")
		mConnect.Uncheck()
	}
}

// applyRegisterStatus updates the Register menu item checkmark and title.
func applyRegisterStatus(enabled bool) {
	if enabled {
		mRegister.SetTitle("LFS Backend Enabled")
		mRegister.Check()
	} else {
		mRegister.SetTitle("Enable LFS Backend")
		mRegister.Uncheck()
	}
}

func applyCredCheckmarks(provider string) {
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
	args := "--backend sdk"
	if prefs.CredentialProvider == config.CredentialProviderGitCredential {
		args += " --credential-provider git-credential"
	}
	if driveCLIPath != "" {
		args += " --drive-cli-bin " + driveCLIPath
	}
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

// sendNotification shows a native macOS notification banner, or falls back
// to notify-send on Linux.
func sendNotification(msg string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "Proton Git LFS"`, msg)).Start()
	case "linux":
		_ = exec.Command("notify-send", "Proton Git LFS", msg).Start()
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
