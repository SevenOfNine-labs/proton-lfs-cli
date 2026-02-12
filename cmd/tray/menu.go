package main

import (
	"embed"
	"fmt"
	"os/exec"
	"runtime"

	"fyne.io/systray"

	"proton-git-lfs/internal/config"
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
	mLastTransfer *systray.MenuItem
	mCredGit      *systray.MenuItem
	mCredPass     *systray.MenuItem
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
	systray.SetTooltip("Proton Git LFS")

	mVersion := systray.AddMenuItem(fmt.Sprintf("Proton Git LFS %s", Version), "")
	mVersion.Disable()

	systray.AddSeparator()

	mStatus = systray.AddMenuItem("Status: Starting…", "")
	mStatus.Disable()
	mLastTransfer = systray.AddMenuItem("Last Transfer: —", "")
	mLastTransfer.Disable()

	systray.AddSeparator()

	mCredMenu := systray.AddMenuItem("Credential Provider", "")
	mCredGit = mCredMenu.AddSubMenuItem("git-credential", "Use Git Credential Manager")
	mCredPass = mCredMenu.AddSubMenuItem("pass-cli", "Use Proton Pass CLI")

	prefs := config.LoadPrefs()
	applyCredCheckmarks(prefs.CredentialProvider)

	systray.AddSeparator()

	mSetup := systray.AddMenuItem("Setup Credentials…", "Open terminal to configure credentials")
	mRegister := systray.AddMenuItem("Register with Git LFS", "Configure git-lfs to use Proton adapter")

	systray.AddSeparator()

	mAutoStart := systray.AddMenuItemCheckbox("Launch at Login", "Start Proton Git LFS when you log in", isAutoStartEnabled())

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit Proton Git LFS tray")

	// Event loop
	go func() {
		for {
			select {
			case <-mCredGit.ClickedCh:
				switchCredentialProvider(config.CredentialProviderGitCredential)
			case <-mCredPass.ClickedCh:
				switchCredentialProvider(config.CredentialProviderPassCLI)
			case <-mSetup.ClickedCh:
				launchCredentialSetup()
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

func launchCredentialSetup() {
	prefs := config.LoadPrefs()
	var cmd *exec.Cmd
	if prefs.CredentialProvider == config.CredentialProviderGitCredential {
		cmd = terminalCommand("echo 'Store credentials with: proton-drive credential store -u <email>' && read -p 'Press Enter to close...'")
	} else {
		cmd = terminalCommand("echo 'Login with: pass-cli login' && read -p 'Press Enter to close...'")
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

func registerGitLFS() {
	adapterPath := discoverAdapterBinary()
	if adapterPath == "" {
		return
	}
	_ = exec.Command("git", "config", "--global", "lfs.customtransfer.proton.path", adapterPath).Run()

	prefs := config.LoadPrefs()
	driveCLIPath := discoverDriveCLIBinary()
	args := "--backend sdk"
	if prefs.CredentialProvider == config.CredentialProviderGitCredential {
		args += " --credential-provider git-credential"
	}
	if driveCLIPath != "" {
		args += " --drive-cli-bin " + driveCLIPath
	}
	_ = exec.Command("git", "config", "--global", "lfs.customtransfer.proton.args", args).Run()
	_ = exec.Command("git", "config", "--global", "lfs.standalonetransferagent", "proton").Run()
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
		return exec.Command("osascript", "-e",
			fmt.Sprintf(`tell application "Terminal" to do script "%s"`, script))
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
