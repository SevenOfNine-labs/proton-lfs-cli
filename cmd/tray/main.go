// Proton Git LFS system tray application.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"fyne.io/systray"
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
		case "config":
			os.Exit(cliConfig(os.Stdout, os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			fmt.Fprint(os.Stderr, usage)
			os.Exit(1)
		}
	}
	if !acquireLock() {
		fmt.Fprintln(os.Stderr, "proton-lfs-cli is already running")
		os.Exit(0)
	}
	augmentPath()
	systray.Run(onReady, onExit)
}

const usage = `Proton Git LFS — system tray app and CLI

Usage:
  proton-lfs-cli                   Launch the system tray app
  proton-lfs-cli login             Authenticate with Proton
  proton-lfs-cli logout            Log out and clear session
  proton-lfs-cli register          Enable LFS backend (git config --global)
  proton-lfs-cli status            Show session, LFS, and transfer status
  proton-lfs-cli config [provider] Show or set credential provider
  proton-lfs-cli --version         Print version and exit
  proton-lfs-cli --help            Show this help

Credential providers: git-credential, pass-cli
`

func onReady() {
	setupMenu()
	startStatusWatcher()
}

// augmentPath inherits the user's full shell PATH so that binaries
// installed via Homebrew, nvm, ~/.local/bin, etc. are available.
// macOS apps launched from Finder/Spotlight get a minimal PATH that
// excludes most user-installed tools.
func augmentPath() {
	out, err := exec.Command("zsh", "-lc", "echo $PATH").Output()
	if err != nil {
		return
	}
	shellPath := strings.TrimSpace(string(out))
	if shellPath != "" {
		_ = os.Setenv("PATH", shellPath)
	}
}

func onExit() {
	stopStatusWatcher()
	releaseLock()
}

// acquireLock prevents multiple instances from running simultaneously.
// Returns true if the lock was acquired, false if another instance is running.
func acquireLock() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return true // can't determine home dir, allow launch
	}
	dir := home + "/.proton-lfs-cli"
	_ = os.MkdirAll(dir, 0o700)
	lockPath := dir + "/tray.lock"

	// Try to create exclusively — fails if file already exists
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		// File exists — check if the PID inside is still alive
		data, readErr := os.ReadFile(lockPath)
		if readErr == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				// Check if process is still running
				if proc, err := os.FindProcess(pid); err == nil {
					if proc.Signal(nil) == nil {
						return false // still alive
					}
				}
			}
		}
		// Stale lock — remove and retry
		_ = os.Remove(lockPath)
		f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return false
		}
	}
	_, _ = fmt.Fprintf(f, "%d", os.Getpid())
	_ = f.Close()
	lockFile = lockPath
	return true
}

var lockFile string

func releaseLock() {
	if lockFile != "" {
		_ = os.Remove(lockFile)
	}
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
