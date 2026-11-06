//go:build !windows

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// configureBridgeProcess places the bridge in its own process group so that
// a timeout kills the helpers it spawned (git credential, pass-cli) as well,
// and bounds the wait for the pipes once the process is gone.
func configureBridgeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killBridgeProcess(cmd) }
	cmd.WaitDelay = 5 * time.Second
}

// killBridgeProcess sends SIGKILL to the bridge's process group.
func killBridgeProcess(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
