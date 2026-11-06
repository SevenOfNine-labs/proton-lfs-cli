package main

import (
	"context"
	"os/exec"
	"time"
)

// driveCLICommandTimeout bounds the proton-drive-cli invocations the tray
// starts on its own (session refresh, logout). Each is one HTTPS round trip
// to the Proton API; a process that never returns must not leave the tray's
// refresh state "in progress" or block the CLI indefinitely.
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
