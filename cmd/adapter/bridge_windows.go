//go:build windows

package main

import (
	"os/exec"
	"time"
)

// configureBridgeProcess bounds the wait for the pipes once the bridge
// process has been killed; Windows has no process groups to propagate the
// kill to helper processes.
func configureBridgeProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

// killBridgeProcess terminates the bridge process.
func killBridgeProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
