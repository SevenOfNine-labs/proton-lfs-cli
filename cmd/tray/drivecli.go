package main

import (
	"context"
	"os/exec"
	"time"
)

// driveCLICommandTimeout bounds the proton-drive-cli invocations the tray
// starts on its own (session refresh, logout), each one Proton API round trip.
const driveCLICommandTimeout = 2 * time.Minute

// driveCLITimeout is the effective timeout; tests shorten it.
var driveCLITimeout = driveCLICommandTimeout

// driveCLICommand builds a proton-drive-cli command bound to ctx, with
// WaitDelay set so pipes inherited by a grandchild cannot stall Wait.
func driveCLICommand(ctx context.Context, driveCLI string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, driveCLI, args...)
	cmd.WaitDelay = subprocessWaitDelay
	return cmd
}
