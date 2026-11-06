// Package main holds the repository helper programs: the doctor-readiness
// checker the Makefile runs before live canaries, and the guarded live
// canary tests.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/preflight"
)

func main() {
	schemaPath := flag.String(
		"schema",
		"submodules/proton-drive-cli/schemas/bridge/v1/auth-state-payload.schema.json",
		"bridge auth-state payload schema path",
	)
	requireLiveCanary := flag.Bool("require-live-canary", false, "require canAttemptLiveCanary=true")
	requireTransfer := flag.Bool("require-transfer", false, "require canAttemptTransfer=true")
	requireState := flag.String("require-state", "", "require an exact authState.state")
	quiet := flag.Bool("quiet", false, "suppress success output")
	flag.Parse()

	states, err := preflight.LoadBridgeAuthStates(*schemaPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read offline doctor JSON: %v\n", err)
		os.Exit(2)
	}

	readiness, err := preflight.ValidateDoctorReadiness(body, states, preflight.DoctorReadinessRequirements{
		RequireLiveCanary: *requireLiveCanary,
		RequireTransfer:   *requireTransfer,
		RequireState:      *requireState,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if !*quiet {
		fmt.Printf(
			"Doctor readiness ok: authState=%s canAttemptTransfer=%t canAttemptLiveCanary=%t\n",
			readiness.AuthState.State,
			readiness.CanAttemptTransfer,
			readiness.CanAttemptLiveCanary,
		)
	}
}
