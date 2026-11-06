package main

import (
	"regexp"
	"strings"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

var shellWordRe = regexp.MustCompile(`^[A-Za-z0-9_@/.-]+$`)

// buildProtonTransferArgs renders lfs.customtransfer.proton.args for the
// adapter: the SDK backend and, when known, the proton-drive-cli path.
func buildProtonTransferArgs(driveCLIPath string) string {
	args := []string{"--backend", config.BackendSDK}
	if driveCLIPath != "" {
		args = append(args, "--drive-cli-bin", driveCLIPath)
	}
	return strings.Join(shellQuoteArgs(args), " ")
}

func shellQuoteArgs(args []string) []string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuoteArg(arg))
	}
	return quoted
}

// shellQuoteArg quotes arg for the POSIX shell that git-lfs uses to split
// lfs.customtransfer.<name>.args: the argument is single-quoted and each
// embedded single quote is closed, backslash-escaped and reopened.
func shellQuoteArg(arg string) string {
	if arg != "" && shellWordRe.MatchString(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
