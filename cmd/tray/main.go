// Proton LFS system tray application.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"
	"github.com/gofrs/flock"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

// Version is stamped at build time via -ldflags.
var Version = "dev"

func main() {
	initTrayLog()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("proton-lfs-cli %s\n", Version)
			return
		case "--help", "-h":
			fmt.Print(usage)
			return
		case "login":
			if hasHelpFlag(os.Args[2:]) {
				fmt.Println("Usage: proton-lfs-cli login\n\nAuthenticate with Proton using the configured credential provider.")
				return
			}
			augmentPath()
			os.Exit(cliLogin(os.Stdout))
		case "logout":
			if hasHelpFlag(os.Args[2:]) {
				fmt.Println("Usage: proton-lfs-cli logout\n\nLog out and clear the current Proton session.")
				return
			}
			augmentPath()
			os.Exit(cliLogout(os.Stdout))
		case "register":
			if hasHelpFlag(os.Args[2:]) {
				fmt.Println("Usage: proton-lfs-cli register\n\nEnable the Proton LFS backend in git global config.")
				return
			}
			augmentPath()
			os.Exit(cliRegister(os.Stdout))
		case "status":
			if hasHelpFlag(os.Args[2:]) {
				fmt.Println("Usage: proton-lfs-cli status\n\nShow session, LFS registration, credential provider, and transfer status.")
				return
			}
			augmentPath()
			os.Exit(cliStatus(os.Stdout))
		case "scope-diagnostics":
			if hasHelpFlag(os.Args[2:]) {
				fmt.Println("Usage: proton-lfs-cli scope-diagnostics [--live]\n\nPrint redacted auth/session and Drive API scope diagnostics. Without --live, no Proton network request is attempted. With --live, the command requires the explicit live canary acknowledgement and performs exactly one read-only bridge list request.")
				return
			}
			augmentPath()
			os.Exit(cliScopeDiagnostics(os.Stdout, os.Args[2:]))
		case "config":
			os.Exit(cliConfig(os.Stdout, os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			fmt.Fprint(os.Stderr, usage)
			os.Exit(1)
		}
	}
	locked, err := acquireInstanceLock()
	if err != nil {
		trayLog.Printf("single-instance lock unavailable, continuing: %v", err)
	} else if !locked {
		fmt.Fprintln(os.Stderr, "proton-lfs-cli is already running")
		os.Exit(0)
	}
	augmentPath()
	systray.Run(onReady, onExit)
}

const usage = `Proton LFS — system tray app and CLI

Usage:
  proton-lfs-cli                   Launch the system tray app
  proton-lfs-cli login             Authenticate with Proton
  proton-lfs-cli logout            Log out and clear session
  proton-lfs-cli register          Enable LFS backend (git config --global)
  proton-lfs-cli status            Show session, LFS, and transfer status
  proton-lfs-cli scope-diagnostics [--live]
                                   Print redacted Drive auth/scope evidence
  proton-lfs-cli config [provider] Show or set credential provider
  proton-lfs-cli --version         Print version and exit
  proton-lfs-cli --help            Show this help

Credential providers: git-credential, pass-cli
`

func onReady() {
	if err := migrateGitLFSRegistration(); err != nil {
		trayLog.Printf("git config migration failed: %v", err)
	}
	setupMenu()
	startStatusWatcher()
}

func onExit() {
	stopStatusWatcher()
	releaseInstanceLock()
}

// loginShellPathTimeout bounds the login shell started by augmentPath. Shell
// startup files can block (for example on a network mount or an interactive
// prompt), and the tray must not hang before its icon appears.
const loginShellPathTimeout = 5 * time.Second

// augmentPath adopts the PATH of the user's login shell on macOS and Linux.
// Apps started from Finder, Spotlight or a LaunchAgent inherit launchd's
// minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin), and XDG autostart entries
// inherit the display manager's environment, which does not run the login
// files of shells such as zsh; both lack Homebrew, nvm and ~/.local/bin
// where pass-cli and node are installed. Windows programs inherit the
// user's PATH from the registry.
func augmentPath() {
	if runtime.GOOS == "windows" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginShellPathTimeout)
	defer cancel()
	shellPath, err := loginShellPath(ctx, os.Getenv("SHELL"))
	if err != nil {
		trayLog.Printf("login shell PATH not adopted: %v", err)
		return
	}
	if shellPath != "" {
		_ = os.Setenv("PATH", shellPath)
	}
}

// loginShellPath runs shell as a login shell and returns the PATH it exports.
// An empty shell falls back to zsh, the macOS default login shell since
// 10.15. Only the last output line is used: login startup files may print
// banners before the shell evaluates the command, and printf '%s' writes the
// PATH without a trailing newline so it is always that last line.
func loginShellPath(ctx context.Context, shell string) (string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		shell = "zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-lc", `printf '%s' "$PATH"`)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}

// trayLockFileName is the advisory lock file inside config.AppDir that marks
// a running tray instance.
const trayLockFileName = "tray.lock"

// instanceLock is held for the lifetime of the tray process. The lock is an
// OS advisory lock (flock(2) on Unix, LockFileEx on Windows) that the kernel
// releases when the process exits, so a crashed instance never leaves a stale
// lock behind.
var instanceLock *flock.Flock

// acquireInstanceLock takes the single-instance lock. It returns false when
// another tray instance holds it.
func acquireInstanceLock() (bool, error) {
	dir, err := config.AppDirPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	lock, locked, err := tryInstanceLock(filepath.Join(dir, trayLockFileName))
	if err != nil || !locked {
		return false, err
	}
	instanceLock = lock
	return true, nil
}

// tryInstanceLock attempts a non-blocking exclusive lock on path. The
// returned Flock must be unlocked by the caller when locked is true.
func tryInstanceLock(path string) (lock *flock.Flock, locked bool, err error) {
	lock = flock.New(path)
	locked, err = lock.TryLock()
	if err != nil {
		return nil, false, err
	}
	if !locked {
		_ = lock.Close()
		return nil, false, nil
	}
	return lock, true, nil
}

func releaseInstanceLock() {
	if instanceLock == nil {
		return
	}
	_ = instanceLock.Unlock()
	instanceLock = nil
}

// hasHelpFlag returns true if args contains --help or -h.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
