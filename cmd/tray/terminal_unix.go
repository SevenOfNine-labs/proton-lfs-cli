//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
)

// terminalEmulator describes how one Linux terminal emulator receives the
// command to run.
type terminalEmulator struct {
	name string
	args func(script string) []string
}

// linuxTerminalEmulators is tried in order. x-terminal-emulator (Debian
// Policy 11.8.3) and xterm take the program after -e; gnome-terminal(1)
// takes it after --.
var linuxTerminalEmulators = []terminalEmulator{
	{name: "x-terminal-emulator", args: func(s string) []string { return []string{"-e", "bash", "-c", s} }},
	{name: "gnome-terminal", args: func(s string) []string { return []string{"--", "bash", "-c", s} }},
	{name: "xterm", args: func(s string) []string { return []string{"-e", "bash", "-c", s} }},
}

// terminalCommand returns a command that opens a terminal window running the
// POSIX shell snippet script, and a cleanup for resources the command owns
// before it starts. It returns nil when no terminal emulator is available.
func terminalCommand(script string) (*exec.Cmd, func()) {
	switch runtime.GOOS {
	case "darwin":
		return terminalCommandDarwin(script)
	case "linux":
		return terminalCommandLinux(script), nil
	default:
		return nil, nil
	}
}

func terminalCommandLinux(script string) *exec.Cmd {
	for _, term := range linuxTerminalEmulators {
		if p, err := exec.LookPath(term.name); err == nil {
			return exec.Command(p, term.args(script)...)
		}
	}
	return nil
}

// terminalCommandDarwin writes script to a self-deleting file that
// Terminal.app runs; the path reaches AppleScript as an argv item (`on run
// argv`). The returned cleanup removes the file if the command never starts.
func terminalCommandDarwin(script string) (*exec.Cmd, func()) {
	f, err := os.CreateTemp("", "proton-lfs-*.sh")
	if err != nil {
		return nil, nil
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	content := "#!/bin/zsh\nclear\n" + script + "\nrm -f \"$0\"\n"
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		cleanup()
		return nil, nil
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		cleanup()
		return nil, nil
	}
	cmd := exec.Command("osascript",
		"-e", "on run argv",
		"-e", `tell application "Terminal" to do script (item 1 of argv)`,
		"-e", "end run",
		f.Name())
	return cmd, cleanup
}
