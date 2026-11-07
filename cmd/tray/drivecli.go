package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// driveCLICommandTimeout bounds the proton-drive-cli invocations the tray
// starts on its own (session refresh, logout): longer than proton-drive-cli's
// two-minute session-lock wait plus a credential-store save.
const driveCLICommandTimeout = 5 * time.Minute

// driveCLITimeout is the effective timeout; tests shorten it.
var driveCLITimeout = driveCLICommandTimeout

// driveCLICommand builds a proton-drive-cli command bound to ctx, with
// WaitDelay set so pipes inherited by a grandchild cannot stall Wait, and
// the environment of driveCLIEnv.
func driveCLICommand(ctx context.Context, driveCLI string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, driveCLI, args...)
	cmd.WaitDelay = subprocessWaitDelay
	cmd.Env = driveCLIEnv(os.Environ())
	return cmd
}

// driveCLIEnv returns env with PROTON_PASS_CLI_BIN set, unless env sets it
// non-blank, to the pass-cli the tray finds: every proton-drive-cli command
// may read the session's credential-store entry; launchd's PATH lacks pass-cli.
func driveCLIEnv(env []string) []string {
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if envKeyEqual(key, "PROTON_PASS_CLI_BIN") && strings.TrimSpace(value) != "" {
			return env
		}
	}
	if passCLI := discoverPassCLIBinary(); passCLI != "" {
		return append(env, "PROTON_PASS_CLI_BIN="+passCLI)
	}
	return env
}

// envKeyEqual compares environment variable names, case-insensitively on
// Windows (SetEnvironmentVariable, Microsoft Learn).
func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
