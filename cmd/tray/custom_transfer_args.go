package main

import (
	"regexp"
	"strings"
)

var shellWordRe = regexp.MustCompile(`^[A-Za-z0-9_@/.-]+$`)

func buildProtonTransferArgs(_ string, driveCLIPath string) string {
	args := []string{"--backend", "sdk"}
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

func shellQuoteArg(arg string) string {
	if arg != "" && shellWordRe.MatchString(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
