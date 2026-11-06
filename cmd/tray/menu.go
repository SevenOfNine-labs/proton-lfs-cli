package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"

	"fyne.io/systray"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

//go:embed icons/*.png
var iconFS embed.FS

// The icon set has four states: idle (grey), ok (green), error (red) and
// syncing (blue). Status states without an icon of their own (rate_limited,
// auth_required, captcha) use the error icon and are told apart by tooltip.
var (
	iconIdle    []byte
	iconOK      []byte
	iconError   []byte
	iconSyncing []byte
)

// Menu items that get updated dynamically.
var (
	mStatus      *systray.MenuItem
	mSession     *systray.MenuItem
	mTransfers   *systray.MenuItem
	mRefresh     *systray.MenuItem
	mPrimaryAuth *systray.MenuItem
	mRecheck     *systray.MenuItem
	mCredGit     *systray.MenuItem
	mCredPass    *systray.MenuItem
	mRegister    *systray.MenuItem
	mOpenLogs    *systray.MenuItem
	mDoctor      *systray.MenuItem

	// currentAuthAction holds the authMenuAction the poll goroutine last
	// applied; the menu goroutine reads it.
	currentAuthAction atomic.Value
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
	mRecheck = systray.AddMenuItem("Recheck Status", "Refresh tray readiness state")

	systray.AddSeparator()

	mCredPass = systray.AddMenuItemCheckbox("Use Proton Pass", "The next Connect to Proton stores the key password in Proton Pass CLI", false)
	mCredGit = systray.AddMenuItemCheckbox("Use Git Credential Manager", "The next Connect to Proton stores the key password in the OS credential manager", false)

	prefs := config.LoadPrefs()
	applyCredCheckmarks(prefs.KeyPasswordProvider)

	systray.AddSeparator()

	mRegister = systray.AddMenuItemCheckbox("Enable LFS Backend", "Configure Git to route LFS transfers through Proton Drive", false)

	systray.AddSeparator()

	mOpenLogs = systray.AddMenuItem("Open Logs", "Open a terminal tailing Proton LFS tray logs")
	mDoctor = systray.AddMenuItem("Run Doctor...", "Open offline Proton Drive doctor checks")
	mAutoStart := systray.AddMenuItemCheckbox("Start at System Login", "Automatically launch the tray app when you log in to your computer", isAutoStartEnabled())

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit Proton LFS", "Quit Proton LFS tray")

	go func() {
		for {
			select {
			case <-mCredGit.ClickedCh:
				switchCredentialProvider(config.CredentialProviderGitCredential)
			case <-mCredPass.ClickedCh:
				switchCredentialProvider(config.CredentialProviderPassCLI)
			case <-mPrimaryAuth.ClickedCh:
				handlePrimaryAuthAction()
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
	action, _ := currentAuthAction.Load().(authMenuAction)
	switch action {
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
	prefs.KeyPasswordProvider = provider
	_ = config.SavePrefs(prefs)
	applyCredCheckmarks(provider)
}

// registerGitLFS writes the same global git settings as
// `proton-lfs-cli register`.
func registerGitLFS() {
	adapterPath := findAdapter()
	if adapterPath == "" {
		sendNotification("Error: adapter binary not found")
		return
	}
	if err := writeGitLFSRegistration(adapterPath, buildProtonTransferArgs(findDriveCLI())); err != nil {
		trayLog.Printf("register: %v", err)
		sendNotification("Error: git config failed")
		return
	}
	applyRegisterStatus(true)
	sendNotification("LFS Backend Enabled")
}

// isLFSEnabled checks whether the Proton LFS adapter is registered in git global config.
func isLFSEnabled() bool {
	return readGitLFSConfig().StandaloneAgent == gitStandaloneAgent
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
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		trayLog.Print("disconnect: proton-drive-cli binary not found")
		sendNotification("Error: CLI not found")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), driveCLITimeout)
		defer cancel()
		out, err := driveCLICommand(ctx, driveCLI, "logout").CombinedOutput()
		logSubprocessOutput("disconnect", out)
		if err != nil {
			trayLog.Printf("disconnect: logout failed: %v", err)
			sendNotification("Disconnect failed")
			return
		}
		trayLog.Print("disconnect: logout succeeded")
		clearRefreshState()
		sendNotification("Disconnected from Proton")
		refreshTrayState()
	}()
}

func recheckStatus() {
	refreshTrayState()
	maybeRefreshSession()
}

func openLogs() {
	logPath := trayLogPath()
	openInTerminal(fmt.Sprintf("tail -n 200 -f %s", shellQuoteArg(logPath)), "Logs: "+logPath)
}

func runDoctor() {
	driveCLI := findDriveCLI()
	if driveCLI == "" {
		sendNotification("Error: CLI not found")
		return
	}
	// Without --key-password-provider, doctor checks the provider and host
	// recorded in the session (proton-drive-cli src/cli/doctor.ts).
	args := []string{shellQuoteArg(driveCLI), "doctor"}
	openInTerminal(strings.Join(args, " ")+"; echo; echo 'Press Enter to close...'; read -r",
		"Doctor could not open terminal")
}

// sendNotification shows a desktop notification: a macOS banner through
// osascript or notify-send on Linux. Every message is also written to the
// tray log, which is the only channel on platforms without a notifier.
func sendNotification(msg string) {
	trayLog.Printf("notify: %s", msg)
	switch runtime.GOOS {
	case "darwin":
		_ = darwinNotificationCommand(msg).Start()
	case "linux":
		_ = exec.Command("notify-send", "Proton LFS", msg).Start()
	}
}

// darwinNotificationCommand passes msg to the AppleScript as an argv item
// (`on run argv`), never by interpolating it into the script source, so
// quotes and backslashes in msg cannot alter the script.
func darwinNotificationCommand(msg string) *exec.Cmd {
	return exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display notification (item 1 of argv) with title "Proton LFS"`,
		"-e", "end run",
		msg)
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

// openInTerminal runs the POSIX shell snippet script in a terminal window
// and shows fallback as a notification when no terminal can be opened.
func openInTerminal(script, fallback string) {
	cmd, cleanup := terminalCommand(script)
	if cmd == nil {
		sendNotification(fallback)
		return
	}
	launchTerminal(cmd, cleanup, func() { sendNotification(fallback) })
}

// launchTerminal starts cmd and calls onFailure when the start fails or the
// terminal exits non-zero; xterm exits only when its window closes. cleanup
// runs only when cmd never started, since a started command cleans up itself.
func launchTerminal(cmd *exec.Cmd, cleanup func(), onFailure func()) {
	if err := cmd.Start(); err != nil {
		trayLog.Printf("terminal: start failed: %v", err)
		if cleanup != nil {
			cleanup()
		}
		onFailure()
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			trayLog.Printf("terminal: %s exited: %v", cmd.Path, err)
			onFailure()
		}
	}()
}
