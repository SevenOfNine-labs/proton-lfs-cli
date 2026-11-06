//go:build windows

package main

import "os/exec"

// terminalCommand returns no command on Windows: the tray's helper snippets
// are POSIX shell (tail -f, read -r) and Windows ships no POSIX shell, so
// callers fall back to sendNotification, which logs the message.
func terminalCommand(string) (*exec.Cmd, func()) {
	return nil, nil
}
